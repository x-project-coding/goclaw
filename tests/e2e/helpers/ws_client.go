//go:build e2e

package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient wraps gorilla/websocket for the GoClaw `req`/`res`/`event` protocol.
// Frames carry an `id` for request/response correlation; we generate IDs via an
// atomic counter and dispatch incoming frames to per-id channels.
type WSClient struct {
	conn   *websocket.Conn
	nextID atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan wsResult
	events  chan WSEvent
	closed  atomic.Bool
}

// WSFrame is the wire envelope. Matches pkg/protocol/frames.go ResponseFrame /
// EventFrame / RequestFrame as a single unmarshal target. Notable wire fields:
//
//	type=res  → ID + OK + Payload + Error
//	type=event → Event + Payload (+ Seq + StateVersion)
//	type=req  → ID + Method + Params (client → server)
type WSFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	OK      bool            `json:"ok,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Event   string          `json:"event,omitempty"`
	Error   *WSError        `json:"error,omitempty"`
}

// WSError mirrors pkg/protocol/frames.go ErrorShape.
type WSError struct {
	Code         string          `json:"code"`
	Message      string          `json:"message"`
	Details      json.RawMessage `json:"details,omitempty"`
	Retryable    bool            `json:"retryable,omitempty"`
	RetryAfterMs int             `json:"retryAfterMs,omitempty"`
}

func (e *WSError) Error() string { return fmt.Sprintf("ws %s: %s", e.Code, e.Message) }

// WSEvent is delivered to subscribers via WaitEvent.
// Payload mirrors EventFrame.Payload from pkg/protocol/frames.go.
type WSEvent struct {
	Event   string
	Payload json.RawMessage
}

// NewWSClient dials ws://host:port/ws using the supplied JWT (optional) for auth.
// On success, kicks off a reader goroutine that demuxes responses + events.
func NewWSClient(ctx context.Context, jwt string) (*WSClient, error) {
	MustLoadEnv()
	u := url.URL{Scheme: "ws", Host: GatewayHost() + ":" + GatewayPort(), Path: "/ws"}
	header := http.Header{}
	if jwt != "" {
		header.Set("Authorization", "Bearer "+jwt)
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, _, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return nil, fmt.Errorf("ws dial %s: %w", u.String(), err)
	}
	c := &WSClient{
		conn:    conn,
		pending: make(map[uint64]chan wsResult),
		events:  make(chan WSEvent, 64),
	}
	go c.readLoop()
	return c, nil
}

// Connect sends the mandatory `connect` frame as the first request.
// Caller passes any session-bootstrap params the server expects.
func (c *WSClient) Connect(ctx context.Context, params map[string]any) (json.RawMessage, error) {
	return c.SendReq(ctx, "connect", params)
}

// SendReq sends a `req` frame and blocks until the matching `res` arrives or ctx fires.
// Returns the response payload on ok=true, or a WSError on ok=false.
func (c *WSClient) SendReq(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("ws: connection closed")
	}
	id := c.nextID.Add(1)
	idStr := fmt.Sprintf("e2e-%d", id)
	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	frame := WSFrame{Type: "req", ID: idStr, Method: method, Params: paramsRaw}

	resCh := make(chan wsResult, 1)
	c.mu.Lock()
	c.pending[id] = resCh
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	c.mu.Lock()
	err = c.conn.WriteJSON(&frame)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case r := <-resCh:
		if r.err != nil {
			return nil, r.err
		}
		return r.payload, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// wsResult bundles a payload-or-error reply for the request multiplexer.
type wsResult struct {
	payload json.RawMessage
	err     error
}

// WaitEvent blocks until an event of the given name arrives or timeout fires.
// Set eventName="" to accept any event.
func (c *WSClient) WaitEvent(eventName string, timeout time.Duration) (WSEvent, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return WSEvent{}, fmt.Errorf("ws: events channel closed")
			}
			if eventName == "" || ev.Event == eventName {
				return ev, nil
			}
		case <-deadline.C:
			return WSEvent{}, fmt.Errorf("ws: WaitEvent(%q) timeout after %s", eventName, timeout)
		}
	}
}

// Close shuts down the underlying connection and the reader goroutine.
func (c *WSClient) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	return c.conn.Close()
}

func (c *WSClient) readLoop() {
	for {
		var frame WSFrame
		if err := c.conn.ReadJSON(&frame); err != nil {
			c.closed.Store(true)
			c.failPending(err)
			close(c.events)
			return
		}
		switch frame.Type {
		case "res":
			c.dispatchResponse(frame)
		case "event":
			select {
			case c.events <- WSEvent{Event: frame.Event, Payload: frame.Payload}:
			default:
				// Drop event if buffer full — tests should drain in time.
			}
		default:
			// Unknown frame type — log via stderr but don't fail the loop.
		}
	}
}

func (c *WSClient) dispatchResponse(frame WSFrame) {
	var id uint64
	if _, err := fmt.Sscanf(frame.ID, "e2e-%d", &id); err != nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return
	}
	if !frame.OK {
		var err error = frame.Error
		if frame.Error == nil {
			err = fmt.Errorf("ws: response ok=false with no error envelope")
		}
		ch <- wsResult{err: err}
		return
	}
	ch <- wsResult{payload: frame.Payload}
}

// failPending uses non-blocking sends — sender holds c.mu but receiver may have
// returned via ctx cancellation, leaving the unbuffered channel un-drained. Drop
// the result rather than deadlock; the SendReq side already returns ctx.Err().
func (c *WSClient) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	res := wsResult{err: fmt.Errorf("ws closed: %w", err)}
	for id, ch := range c.pending {
		select {
		case ch <- res:
		default:
		}
		delete(c.pending, id)
	}
}

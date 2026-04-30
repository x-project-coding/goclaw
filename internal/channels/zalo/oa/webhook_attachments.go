package oa

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// oaAttachment is a single attachment item inside the Zalo OA event payload.
// Image / file / sticker / gif / link events all share this shape; the
// per-type fields below are populated selectively by Zalo.
type oaAttachment struct {
	Type    string             `json:"type"`
	Payload oaAttachmentPayload `json:"payload"`
}

// oaAttachmentPayload covers fields seen across image / file / sticker /
// gif / link events. URL is universal; the rest are best-effort.
type oaAttachmentPayload struct {
	URL         string `json:"url,omitempty"`
	Thumbnail   string `json:"thumbnail,omitempty"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// firstAttachmentURL returns the URL of the first attachment with a
// non-empty Payload.URL. Empty when the event has no attachments.
func firstAttachmentURL(atts []oaAttachment) string {
	for _, a := range atts {
		if a.Payload.URL != "" {
			return a.Payload.URL
		}
	}
	return ""
}

// firstAttachment returns a pointer to the first attachment (or nil).
// Useful for link events where we need the title/description, not just URL.
func firstAttachment(atts []oaAttachment) *oaAttachment {
	if len(atts) == 0 {
		return nil
	}
	return &atts[0]
}

// dispatchWebhookMedia downloads the first attachment URL and forwards it
// as a MediaInfo-tagged inbound. Used for user_send_image, user_send_gif,
// user_send_sticker, user_send_file. Sticker / gif are classified as image
// regardless of MIME so the agent treats them visually.
func (c *Channel) dispatchWebhookMedia(e *oaInboundEvent, forceImageKind bool) {
	if e.Sender.ID == "" {
		return
	}
	url := firstAttachmentURL(e.Message.Attachments)
	if url == "" {
		slog.Warn("zalo_oa.webhook.attachment_missing_url",
			"event", e.EventName, "message_id", e.messageID())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	path, err := downloadOAMediaFn(ctx, url)
	if err != nil {
		slog.Warn("zalo_oa.webhook.attachment_download_failed",
			"event", e.EventName, "message_id", e.messageID(), "url", url, "error", err)
		return
	}

	mimeType := media.DetectMIMEType(path)
	kind := media.MediaKindFromMime(mimeType)
	if forceImageKind {
		kind = media.TypeImage
	}

	att := firstAttachment(e.Message.Attachments)
	fileName := ""
	if att != nil {
		fileName = att.Payload.Name
		if fileName == "" {
			fileName = att.Payload.Title
		}
	}

	tag := media.BuildMediaTags([]media.MediaInfo{{
		Type:        kind,
		FilePath:    path,
		ContentType: mimeType,
		FileName:    fileName,
		SourceURL:   url,
	}})

	// Combine the user's caption (Message.Text) with the media tag so the
	// agent sees both. Zalo file/image events often carry an empty Text.
	content := strings.TrimSpace(e.Message.Text)
	if content == "" {
		content = tag
	} else {
		content = content + "\n" + tag
	}

	metadata := common.InboundMeta{
		MessageID:         e.messageID(),
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: e.Sender.DisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(e.Sender.ID, e.Sender.ID, content, []string{path}, metadata, "direct")
}

// dispatchWebhookLink forwards a shared-link event as plain text so the
// agent can decide whether to follow up. We don't fetch the URL — link
// previews are out of scope for this layer (and would risk SSRF on
// arbitrary user-shared URLs).
func (c *Channel) dispatchWebhookLink(e *oaInboundEvent) {
	if e.Sender.ID == "" {
		return
	}
	att := firstAttachment(e.Message.Attachments)
	if att == nil || att.Payload.URL == "" {
		// No structured link — fall back to whatever Text Zalo provided.
		if strings.TrimSpace(e.Message.Text) != "" {
			c.dispatchWebhookText(e)
		}
		return
	}

	var b strings.Builder
	if t := strings.TrimSpace(e.Message.Text); t != "" {
		b.WriteString(t)
		b.WriteString("\n\n")
	}
	b.WriteString("[link] ")
	if att.Payload.Title != "" {
		b.WriteString(att.Payload.Title)
		b.WriteString(" — ")
	}
	b.WriteString(att.Payload.URL)
	if att.Payload.Description != "" {
		b.WriteString("\n")
		b.WriteString(att.Payload.Description)
	}

	metadata := common.InboundMeta{
		MessageID:         e.messageID(),
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: e.Sender.DisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(e.Sender.ID, e.Sender.ID, b.String(), nil, metadata, "direct")
}

// oaWebhookMaxMediaBytes caps incoming attachment downloads. Matches the
// 20 MB default used by other channels (telegram, zalo_personal).
const oaWebhookMaxMediaBytes = 20 * 1024 * 1024

// downloadOAMediaFn is the package-level downloader; tests swap it so
// httptest loopback URLs aren't blocked by SSRF.
var downloadOAMediaFn = downloadOAMedia

// downloadOAMedia fetches a Zalo CDN URL into a temp file. SSRF-checked,
// size-capped, timeout-bounded. Returns the local path.
func downloadOAMedia(ctx context.Context, fileURL string) (string, error) {
	if err := tools.CheckSSRF(fileURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	client := &http.Client{Timeout: 0} // ctx governs deadline
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	ext := extFromURL(fileURL)
	tmpFile, err := os.CreateTemp("", "goclaw_zoa_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, oaWebhookMaxMediaBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > oaWebhookMaxMediaBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("attachment too large: %d bytes (cap %d)", written, oaWebhookMaxMediaBytes)
	}
	return tmpFile.Name(), nil
}

// extFromURL derives a sane file extension from a URL path; falls back to
// ".bin" for opaque URLs (e.g. CDN links without an extension).
func extFromURL(fileURL string) string {
	path := fileURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ext := filepath.Ext(path)
	if ext == "" || len(ext) > 6 {
		return ".bin"
	}
	return ext
}

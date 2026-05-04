package gateway

import (
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
)

// NewTestClient returns a minimally-wired Client for unit tests in other
// packages. Role is set directly because the underlying fields are unexported.
// SendResponse is safe because the send channel is nil — the writer hits the
// default branch of the select and drops the frame silently.
//
// Not for production use. Any non-test caller should use NewClient instead.
func NewTestClient(role permissions.Role, userID string) *Client {
	return &Client{
		id:            uuid.NewString(),
		authenticated: true,
		role:          role,
		userID:        userID,
	}
}

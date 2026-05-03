package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ChannelContact represents a user discovered through channel interactions.
// Global (not per-agent): same person on the same platform = one row.
type ChannelContact struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	ChannelType     string     `json:"channel_type" db:"channel_type"`
	ChannelInstance *string    `json:"channel_instance,omitempty" db:"channel_instance"`
	SenderID        string     `json:"sender_id" db:"sender_id"`
	UserID          *string    `json:"user_id,omitempty" db:"user_id"`
	DisplayName     *string    `json:"display_name,omitempty" db:"display_name"`
	Username        *string    `json:"username,omitempty" db:"username"`
	AvatarURL       *string    `json:"avatar_url,omitempty" db:"avatar_url"`
	PeerKind        *string    `json:"peer_kind,omitempty" db:"peer_kind"`
	ContactType     string     `json:"contact_type" db:"contact_type"` // "user", "group", or "topic"
	ThreadID        *string    `json:"thread_id,omitempty" db:"thread_id"`
	ThreadType      *string    `json:"thread_type,omitempty" db:"thread_type"`
	MergedID        *uuid.UUID `json:"merged_id,omitempty" db:"merged_id"`
	FirstSeenAt     time.Time  `json:"first_seen_at" db:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at" db:"last_seen_at"`
}

// ContactListOpts holds pagination and filter options for listing contacts.
type ContactListOpts struct {
	Search      string // ILIKE on display_name, username, sender_id
	ChannelType string // filter by platform (telegram, discord, etc.)
	PeerKind    string // "direct" or "group"
	ContactType string // "user" or "group"
	Limit       int
	Offset      int
}

// MergeUserAggregateRequest carries the parameters for an atomic merge.
//
// The merge consolidates `SourceUserIDs`' data into `TargetUserID`:
//   1. UPDATE channel_contacts SET merged_id = TargetUserID, merge_audit = MergeAudit
//        WHERE id = ANY(ContactIDs).
//   2. UPDATE agent_sessions SET user_id = TargetUserID
//        WHERE user_id = ANY(SourceUserIDs).   ← R1 fix.
//   3. UPDATE user_context_files SET user_id = TargetUserID
//        WHERE user_id = ANY(SourceUserIDs).
//   4. UPDATE memory_documents SET user_id = TargetUserID
//        WHERE user_id = ANY(SourceUserIDs).
//
// All four UPDATEs share a single *sql.Tx to guarantee atomicity (Finding 10).
type MergeUserAggregateRequest struct {
	ContactIDs    []uuid.UUID // channel_contacts.id rows whose merged_id must flip to TargetUserID
	SourceUserIDs []uuid.UUID // users.id values whose data must move to TargetUserID
	TargetUserID  uuid.UUID   // destination user (must exist + must NOT be a chained merge)
	MergeAudit    []byte      // JSONB blob {merged_by_user_id, merged_at, from_channel_id, ...}
}

// Sentinel errors for merge pre-check failures (Finding 7 — security).
var (
	ErrMergeSourceAlreadyMerged = errors.New("source contact already merged — user→user merge forbidden")
	ErrMergeTargetAlreadyMerged = errors.New("target user already merged into another — chained merges forbidden")
	ErrMergeTargetUserNotFound  = errors.New("target user not found")
)

// ContactStore manages channel contacts (auto-collected user info).
type ContactStore interface {
	// UpsertContact creates or updates a contact. On conflict (channel_type, sender_id, thread_id),
	// updates display_name, username, user_id, channel_instance, peer_kind, thread_type, last_seen_at.
	UpsertContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string) error

	// ListContacts searches contacts with pagination and filters.
	ListContacts(ctx context.Context, opts ContactListOpts) ([]ChannelContact, error)

	// CountContacts returns total matching contacts for the given filters.
	CountContacts(ctx context.Context, opts ContactListOpts) (int, error)

	// GetContactsBySenderIDs returns contacts matching the given sender IDs.
	GetContactsBySenderIDs(ctx context.Context, senderIDs []string) (map[string]ChannelContact, error)

	// GetContactByID returns a single contact by primary key.
	GetContactByID(ctx context.Context, id uuid.UUID) (*ChannelContact, error)

	// GetSenderIDsByContactIDs returns sender_id strings for the given contact UUIDs in one query.
	GetSenderIDsByContactIDs(ctx context.Context, contactIDs []uuid.UUID) ([]string, error)

	// MergeUserAggregate atomically migrates all data from SourceUserIDs to TargetUserID,
	// stamping the affected channel_contacts rows with merged_id + merge_audit. Single TX
	// covers channel_contacts + agent_sessions + user_context_files + memory_documents.
	// Returns ErrMergeSourceAlreadyMerged / ErrMergeTargetAlreadyMerged / ErrMergeTargetUserNotFound
	// for security pre-check failures (Findings 7 + 10).
	MergeUserAggregate(ctx context.Context, req MergeUserAggregateRequest) error

	// ResolveTenantUserID looks up a contact by (channelType, senderID) and, if
	// the contact has been merged, returns the linked user's UUID as string.
	// Returns ("", nil) when the contact is not found or not merged.
	ResolveTenantUserID(ctx context.Context, channelType, senderID string) (string, error)
}

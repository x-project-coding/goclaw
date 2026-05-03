package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ContactMergeHandler exposes the v4 merge-contact endpoint. It is the ONLY
// sanctioned entry point for merging channel contacts into an authenticated
// user. The handler delegates to store.ContactStore.MergeUserAggregate which
// owns a single *sql.Tx covering channel_contacts + agent_sessions +
// user_context_files + memory_documents (Findings 7 + 10).
type ContactMergeHandler struct {
	contactStore store.ContactStore
	usersStore   store.UsersStore
	msgBus       *bus.MessageBus
}

// NewContactMergeHandler constructs the handler. The users store is required
// for the target-user existence pre-check; it may be nil only in unit tests.
func NewContactMergeHandler(cs store.ContactStore, us store.UsersStore, msgBus *bus.MessageBus) *ContactMergeHandler {
	return &ContactMergeHandler{contactStore: cs, usersStore: us, msgBus: msgBus}
}

// RegisterRoutes registers the merge endpoint. RoleAdmin is required: a member
// can never merge contacts on someone else's behalf.
func (h *ContactMergeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/contacts/merge", h.adminAuth(h.handleMerge))
}

func (h *ContactMergeHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

// mergeRequest is the JSON payload accepted by POST /v1/contacts/merge.
type mergeRequest struct {
	ContactIDs   []string `json:"contact_ids"`
	TargetUserID string   `json:"target_user_id"`
}

func (h *ContactMergeHandler) handleMerge(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	var req mergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if len(req.ContactIDs) == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgContactIDsRequired))
		return
	}
	if req.TargetUserID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMergeTargetRequired))
		return
	}

	contactIDs, err := parseUUIDList(req.ContactIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "contact_id"))
		return
	}
	targetID, err := uuid.Parse(req.TargetUserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "target_user_id"))
		return
	}

	sourceUserIDs, fromChannel, fromSender, err := h.collectMergeSource(ctx, contactIDs, targetID, locale, w)
	if err != nil {
		return // collectMergeSource already wrote the response
	}

	mergeAudit := buildMergeAudit(r, fromChannel, fromSender)
	auditBytes, _ := json.Marshal(mergeAudit) // map[string]any is always marshallable

	if err := h.contactStore.MergeUserAggregate(ctx, store.MergeUserAggregateRequest{
		ContactIDs:    contactIDs,
		SourceUserIDs: sourceUserIDs,
		TargetUserID:  targetID,
		MergeAudit:    auditBytes,
	}); err != nil {
		writeMergeError(w, locale, err)
		return
	}

	emitAudit(h.msgBus, r, "contact.merge_executed", "channel_contacts", targetID.String())

	writeJSON(w, http.StatusOK, map[string]any{
		"target_user_id":  targetID,
		"contact_ids":     contactIDs,
		"source_user_ids": sourceUserIDs,
		"merged_at":       mergeAudit["merged_at"],
	})
}

// collectMergeSource loads the source contacts, derives the distinct source
// user IDs to migrate, and verifies the target user exists. Returns sourceUserIDs
// + provenance (channel + sender of the first contact) for the audit blob.
// Writes the HTTP error directly when validation fails.
func (h *ContactMergeHandler) collectMergeSource(
	ctx context.Context,
	contactIDs []uuid.UUID,
	targetID uuid.UUID,
	locale string,
	w http.ResponseWriter,
) (sourceUserIDs []uuid.UUID, fromChannel, fromSender string, err error) {
	dedup := make(map[uuid.UUID]struct{}, len(contactIDs))
	for _, cid := range contactIDs {
		c, getErr := h.contactStore.GetContactByID(ctx, cid)
		if getErr != nil {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "contact", cid.String()))
			return nil, "", "", getErr
		}
		if c.MergedID != nil {
			writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMergeSourceAlreadyMerged))
			return nil, "", "", store.ErrMergeSourceAlreadyMerged
		}
		if fromChannel == "" {
			fromChannel = c.ChannelType
			fromSender = c.SenderID
		}
		if c.UserID == nil || *c.UserID == "" {
			continue
		}
		uid, parseErr := uuid.Parse(*c.UserID)
		if parseErr != nil || uid == targetID {
			continue
		}
		dedup[uid] = struct{}{}
	}

	if h.usersStore != nil {
		if _, getErr := h.usersStore.Get(ctx, targetID); getErr != nil {
			if errors.Is(getErr, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, protocol.ErrNotFound,
					i18n.T(locale, i18n.MsgMergeTargetUserNotFound, targetID.String()))
				return nil, "", "", store.ErrMergeTargetUserNotFound
			}
			slog.Error("contact_merge.target_lookup", "error", getErr, "target", targetID)
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, "user lookup"))
			return nil, "", "", getErr
		}
	}

	sourceUserIDs = make([]uuid.UUID, 0, len(dedup))
	for uid := range dedup {
		sourceUserIDs = append(sourceUserIDs, uid)
	}
	return sourceUserIDs, fromChannel, fromSender, nil
}

// writeMergeError maps store sentinel errors to HTTP status + i18n message.
func writeMergeError(w http.ResponseWriter, locale string, err error) {
	switch {
	case errors.Is(err, store.ErrMergeSourceAlreadyMerged):
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMergeSourceAlreadyMerged))
	case errors.Is(err, store.ErrMergeTargetAlreadyMerged):
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMergeTargetAlreadyMerged))
	case errors.Is(err, store.ErrMergeTargetUserNotFound):
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgMergeTargetUserNotFound, ""))
	default:
		slog.Error("contact_merge.atomic_failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgMergeAtomicFailed, "transaction failed"))
	}
}

// buildMergeAudit composes the provenance JSON stored on channel_contacts.merge_audit.
// `merged_by_user_id` is taken from the authenticated request context (admin caller).
func buildMergeAudit(r *http.Request, fromChannel, fromSender string) map[string]any {
	merger := store.UserIDFromContext(r.Context())
	return map[string]any{
		"merged_by_user_id": merger,
		"merged_at":         time.Now().UTC().Format(time.RFC3339Nano),
		"from_channel":      fromChannel,
		"from_sender":       fromSender,
	}
}

// parseUUIDList parses a slice of strings into UUIDs, rejecting on the first
// malformed entry to keep error messages predictable.
func parseUUIDList(in []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(in))
	for _, s := range in {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

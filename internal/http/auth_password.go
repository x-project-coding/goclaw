package http

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/auth"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// dummyArgon2idHash is consumed when GetByEmail returns no row, so login
// timing remains constant regardless of whether the email exists. This is the
// PHC encoding of HashPassword("dummy-password-for-timing-equalization").
//
// Computed once at init() to avoid recomputing per request.
var dummyArgon2idHash string

func init() {
	h, err := auth.HashPassword("dummy-password-for-timing-equalization")
	if err == nil {
		dummyArgon2idHash = h
	}
}

// AuthHandler handles password-based auth lifecycle:
//
//	POST /v1/auth/login
//	POST /v1/auth/refresh
//	POST /v1/auth/logout
//	GET  /v1/auth/me
type AuthHandler struct {
	users      store.UsersStore
	sessions   store.UserSessionsStore
	jwtKs      *auth.JWTKeyset
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(
	users store.UsersStore,
	sessions store.UserSessionsStore,
	jwtKs *auth.JWTKeyset,
	accessTTL, refreshTTL time.Duration,
) *AuthHandler {
	return &AuthHandler{
		users:      users,
		sessions:   sessions,
		jwtKs:      jwtKs,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// RegisterRoutes mounts the four auth endpoints on mux.
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/auth/login", loginRateLimitMiddleware(h.handleLogin))
	mux.HandleFunc("/v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("/v1/auth/logout", requireAuth(permissions.RoleViewer, h.handleLogout))
	mux.HandleFunc("/v1/auth/me", requireAuth(permissions.RoleViewer, h.handleMe))
}

type loginBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	locale := extractLocale(r)
	var body loginBody
	if !bindJSON(w, r, locale, &body) {
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))

	// Constant-shape failure response — used for unknown email, wrong password,
	// suspended user. Hides which case triggered (no enumeration).
	failResp := func() {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "invalid_credentials",
			"message": i18n.T(locale, i18n.MsgInvalidCredentials),
		})
	}

	user, err := h.users.GetByEmail(r.Context(), body.Email)
	// Always run VerifyPassword to equalize timing — even when user not found.
	hashToCheck := dummyArgon2idHash
	userActive := false
	if err == nil && user != nil {
		hashToCheck = user.PasswordHash
		userActive = user.Status == "active" && user.DeletedAt == nil
	}
	ok, _ := auth.VerifyPassword(body.Password, hashToCheck)
	if !ok || user == nil || !userActive {
		emailHash := sha256.Sum256([]byte(body.Email))
		slog.Warn("security.auth.login_failed",
			"email_hash", hex.EncodeToString(emailHash[:8]),
			"ip", r.RemoteAddr,
		)
		failResp()
		return
	}

	familyID, err := uuid.NewV7()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "uuid error"})
		return
	}
	rawRefresh, _, err := auth.IssueRefresh(r.Context(), h.sessions, user.ID, familyID, h.refreshTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	accessToken, err := auth.IssueAccess(h.jwtKs, user.ID.String(), permissions.Role(user.Role), h.accessTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error"})
		return
	}
	slog.Info("auth.login_success", "user_id", user.ID, "ip", r.RemoteAddr)

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": rawRefresh,
		"user_id":       user.ID.String(),
		"role":          user.Role,
	})
}

type refreshBody struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	locale := extractLocale(r)
	var body refreshBody
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "refresh_token_required",
			"message": i18n.T(locale, i18n.MsgRefreshTokenInvalid),
		})
		return
	}

	rawNew, newSess, err := auth.RotateRefresh(r.Context(), h.sessions, body.RefreshToken, h.refreshTTL)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "refresh_failed",
			"message": refreshErrMessage(locale, err),
		})
		return
	}

	user, err := h.users.Get(r.Context(), newSess.UserID)
	if err != nil || user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "user_not_found",
			"message": i18n.T(locale, i18n.MsgRefreshTokenInvalid),
		})
		return
	}
	accessToken, err := auth.IssueAccess(h.jwtKs, user.ID.String(), permissions.Role(user.Role), h.accessTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": rawNew,
	})
}

// refreshErrMessage maps auth.RotateRefresh errors to localized messages.
func refreshErrMessage(locale string, err error) string {
	switch {
	case errors.Is(err, auth.ErrRefreshExpired):
		return i18n.T(locale, i18n.MsgRefreshTokenExpired)
	case errors.Is(err, auth.ErrRefreshRevoked):
		return i18n.T(locale, i18n.MsgRefreshTokenRevoked)
	default:
		return i18n.T(locale, i18n.MsgRefreshTokenInvalid)
	}
}

func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	uidStr := store.UserIDFromContext(r.Context())
	if uidStr == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no user"})
		return
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	if err := auth.RevokeAllForUser(r.Context(), h.sessions, uid); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "logout failed"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	uidStr := store.UserIDFromContext(r.Context())
	if uidStr == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no user"})
		return
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	user, err := h.users.Get(r.Context(), uid)
	if err != nil || user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	resp := map[string]any{
		"user_id":    user.ID.String(),
		"email":      user.Email,
		"role":       user.Role,
		"status":     user.Status,
		"created_at": user.CreatedAt,
	}
	if user.DisplayName != nil {
		resp["display_name"] = *user.DisplayName
	}
	writeJSON(w, http.StatusOK, resp)
}

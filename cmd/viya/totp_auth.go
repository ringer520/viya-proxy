package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// totpChallengeTTL bounds how long the password step stays usable before
	// the operator must start over.
	totpChallengeTTL = 5 * time.Minute
	// totpPendingTTL bounds the gap between showing a secret and confirming it.
	totpPendingTTL = 10 * time.Minute
	totpMaxPending = 1000
	totpIssuer     = "Viya"
)

type totpChallenge struct {
	userID   int64
	username string
	expires  time.Time
}

type pendingTOTP struct {
	secret  string
	expires time.Time
}

// totpState holds short-lived, in-memory hand-off state for the two-factor
// flow. Losing it on restart is acceptable: the operator simply signs in again.
type totpState struct {
	mu         sync.Mutex
	challenges map[string]totpChallenge
	pending    map[int64]pendingTOTP
}

func (s *totpState) issueChallenge(userID int64, username string) (string, error) {
	secret, err := randomSecret()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.challenges == nil {
		s.challenges = make(map[string]totpChallenge)
	}
	now := time.Now()
	for key, entry := range s.challenges {
		if now.After(entry.expires) {
			delete(s.challenges, key)
		}
	}
	if len(s.challenges) > totpMaxPending {
		s.challenges = make(map[string]totpChallenge)
	}
	s.challenges[secret] = totpChallenge{userID: userID, username: username, expires: now.Add(totpChallengeTTL)}
	return secret, nil
}

// peekChallenge looks up a challenge without consuming it, so a mistyped code
// does not force a full re-login.
func (s *totpState) peekChallenge(token string) (totpChallenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.challenges[token]
	if !ok || time.Now().After(entry.expires) {
		return totpChallenge{}, false
	}
	return entry, true
}

func (s *totpState) dropChallenge(token string) {
	s.mu.Lock()
	delete(s.challenges, token)
	s.mu.Unlock()
}

func (s *totpState) setPending(userID int64, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = make(map[int64]pendingTOTP)
	}
	now := time.Now()
	for key, entry := range s.pending {
		if now.After(entry.expires) {
			delete(s.pending, key)
		}
	}
	s.pending[userID] = pendingTOTP{secret: secret, expires: now.Add(totpPendingTTL)}
}

func (s *totpState) takePending(userID int64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.pending[userID]
	if !ok {
		return "", false
	}
	delete(s.pending, userID)
	if time.Now().After(entry.expires) {
		return "", false
	}
	return entry.secret, true
}

func (a *App) issueTOTPChallenge(userID int64, username string) (string, error) {
	return a.totp.issueChallenge(userID, username)
}

// loginTOTP completes the second factor. The challenge token is single use and
// only issued after the password already verified.
func (a *App) loginTOTP(w http.ResponseWriter, r *http.Request) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if !validOrigin(r, a.trustedProxies) {
		fail(w, http.StatusForbidden, errors.New("request origin rejected"))
		return
	}
	if !a.allowLogin(r) {
		a.recordLoginAudit(r, "", auditBlocked, auditReasonRateLimited)
		w.Header().Set("Retry-After", "300")
		fail(w, http.StatusTooManyRequests, errors.New("too many login attempts; try again later"))
		return
	}
	var input struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	entry, ok := a.totp.peekChallenge(input.Challenge)
	if !ok {
		fail(w, http.StatusUnauthorized, errors.New("verification expired; sign in again"))
		return
	}
	var secret string
	if err := a.db.QueryRowContext(r.Context(), `SELECT totp_secret FROM users WHERE id=?`, entry.userID).Scan(&secret); err != nil || secret == "" {
		a.totp.dropChallenge(input.Challenge)
		fail(w, http.StatusUnauthorized, errors.New("two-factor authentication is no longer active; sign in again"))
		return
	}
	if !verifyTOTP(secret, input.Code, time.Now()) {
		a.recordLoginAudit(r, entry.username, auditFailure, auditReasonTOTP)
		fail(w, http.StatusUnauthorized, errors.New("verification code is incorrect"))
		return
	}
	a.totp.dropChallenge(input.Challenge)
	if err := a.createSessionForUser(w, r, entry.userID); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not start session"))
		return
	}
	a.clearLoginAttempts(r)
	a.recordLoginAudit(r, entry.username, auditSuccess, auditReasonTOTP)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) totpStatus(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var secret string
	if err := a.db.QueryRowContext(r.Context(), `SELECT totp_secret FROM users WHERE username=?`, session.username).Scan(&secret); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("two-factor status unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": secret != ""})
}

// totpSetup mints a secret and holds it in memory. Nothing is written to the
// database until the operator proves the authenticator app works, so a failed
// enrolment cannot lock anybody out.
func (a *App) totpSetup(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var userID int64
	var current string
	if err := a.db.QueryRowContext(r.Context(), `SELECT id,totp_secret FROM users WHERE username=?`, session.username).Scan(&userID, &current); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("two-factor setup unavailable"))
		return
	}
	if current != "" {
		fail(w, http.StatusConflict, errors.New("two-factor authentication is already enabled"))
		return
	}
	secret, err := newTOTPSecret()
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not generate a secret"))
		return
	}
	a.totp.setPending(userID, secret)
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"uri":    totpProvisioningURI(totpIssuer, session.username, secret),
	})
}

func (a *App) totpEnable(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var input struct {
		Code string `json:"code"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var userID int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE username=?`, session.username).Scan(&userID); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("two-factor setup unavailable"))
		return
	}
	secret, ok := a.totp.takePending(userID)
	if !ok {
		fail(w, http.StatusBadRequest, errors.New("setup expired; start again"))
		return
	}
	if !verifyTOTP(secret, input.Code, time.Now()) {
		// Keep the pending secret so the operator can retry the same code.
		a.totp.setPending(userID, secret)
		fail(w, http.StatusBadRequest, errors.New("verification code is incorrect"))
		return
	}
	if _, err := a.db.ExecContext(r.Context(), `UPDATE users SET totp_secret=? WHERE id=?`, secret, userID); err != nil {
		slog.Error("enable TOTP", "error", err)
		fail(w, http.StatusInternalServerError, errors.New("could not enable two-factor authentication"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
}

// totpDisable requires the current password: possession of a live session alone
// should not be enough to strip the second factor.
func (a *App) totpDisable(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var userID int64
	var storedHash []byte
	if err := a.db.QueryRowContext(r.Context(), `SELECT id,password_hash FROM users WHERE username=?`, session.username).Scan(&userID, &storedHash); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("two-factor status unavailable"))
		return
	}
	if bcrypt.CompareHashAndPassword(storedHash, []byte(input.Password)) != nil {
		fail(w, http.StatusUnauthorized, errors.New("current password is incorrect"))
		return
	}
	if _, err := a.db.ExecContext(r.Context(), `UPDATE users SET totp_secret='' WHERE id=?`, userID); err != nil {
		slog.Error("disable TOTP", "error", err)
		fail(w, http.StatusInternalServerError, errors.New("could not disable two-factor authentication"))
		return
	}
	a.recordLoginAudit(r, session.username, auditSuccess, auditReasonTOTP)
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

// decodeJSONBody decodes a small, single JSON object and rejects trailing data.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid request body")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid request body")
	}
	return nil
}

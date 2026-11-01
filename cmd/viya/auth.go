package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionLifetime = 12 * time.Hour

const (
	// loginWindowLength is the rolling window used by login throttling.
	loginWindowLength = 5 * time.Minute
	// loginMaxAttempts is how many attempts a single IP may make per window.
	loginMaxAttempts = 8
	// sessionRenewAfter is how much of a session may elapse before the expiry
	// is pushed forward. Renewing only in the second half keeps the common case
	// to zero extra writes.
	sessionRenewAfter = sessionLifetime / 2
)

// dummyPasswordHash is compared when the username does not exist so both
// branches spend the same bcrypt time.
var dummyPasswordHash = func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("viya-nonexistent-account"), bcrypt.DefaultCost)
	if err != nil {
		panic("bcrypt unavailable: " + err.Error())
	}
	return hash
}()

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *App) authStatus(w http.ResponseWriter, r *http.Request) {
	var count int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("authentication status unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": count == 0})
}

// authorizeSetup permits first-time setup from any address. The setup handler
// remains protected by same-origin validation, login throttling, and the
// atomic single-administrator creation transaction.
func (a *App) authorizeSetup(_ *http.Request) bool { return true }

func (a *App) setupAdmin(w http.ResponseWriter, r *http.Request) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if !validOrigin(r, a.trustedProxies) {
		fail(w, http.StatusForbidden, errors.New("request origin rejected"))
		return
	}
	if !a.authorizeSetup(r) {
		fail(w, http.StatusForbidden, errors.New("initial setup is unavailable"))
		return
	}
	if !a.allowLogin(r) {
		a.recordLoginAudit(r, "", auditBlocked, auditReasonRateLimited)
		w.Header().Set("Retry-After", "300")
		fail(w, http.StatusTooManyRequests, errors.New("too many setup attempts; try again later"))
		return
	}
	var input credentials
	if err := decodeCredentials(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if len(input.Username) < 3 || len(input.Username) > 64 || input.Password == "" || len(input.Password) < 8 || len(input.Password) > 72 {
		fail(w, http.StatusBadRequest, errors.New("username must be 3-64 characters and password 8-72 bytes"))
		return
	}
	for _, char := range input.Username {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
			fail(w, http.StatusBadRequest, errors.New("username may contain only letters, numbers, dot, dash, and underscore"))
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("password hashing failed"))
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, http.StatusInternalServerError, errors.New("account setup failed"))
		return
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("account setup failed"))
		return
	}
	if count != 0 {
		fail(w, http.StatusConflict, errors.New("administrator is already configured"))
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO users(username,password_hash) VALUES(?,?)`, input.Username, hash); err != nil {
		fail(w, http.StatusConflict, errors.New("administrator could not be created"))
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("account setup failed"))
		return
	}
	if err := a.createSession(w, r, input.Username); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("administrator created but session could not be started; log in"))
		return
	}
	a.recordLoginAudit(r, input.Username, auditSuccess, auditReasonSetup)
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
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
	var input credentials
	if err := decodeCredentials(w, r, &input); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var userID int64
	var storedHash []byte
	err := a.db.QueryRowContext(r.Context(), `SELECT id,password_hash FROM users WHERE username=?`, strings.TrimSpace(input.Username)).Scan(&userID, &storedHash)
	if err != nil {
		// Run a throwaway comparison so a missing user costs the same bcrypt
		// time as a wrong password; otherwise response timing enumerates them.
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(input.Password))
		a.recordLoginAudit(r, input.Username, auditFailure, auditReasonUnknownUser)
		fail(w, http.StatusUnauthorized, errors.New("username or password is incorrect"))
		return
	}
	if bcrypt.CompareHashAndPassword(storedHash, []byte(input.Password)) != nil {
		a.recordLoginAudit(r, input.Username, auditFailure, auditReasonPassword)
		fail(w, http.StatusUnauthorized, errors.New("username or password is incorrect"))
		return
	}
	if err := a.createSessionForUser(w, r, userID); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not start session"))
		return
	}
	a.clearLoginAttempts(r)
	a.recordLoginAudit(r, input.Username, auditSuccess, auditReasonPassword)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// decodeJSONBody reads exactly one JSON document. Trailing data is rejected so
// a client cannot smuggle a second payload past the destination struct.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid request body")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid request body")
	}
	return nil
}

func decodeCredentials(w http.ResponseWriter, r *http.Request, dst *credentials) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid credentials request")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid credentials request")
	}
	if dst.Username == "" || dst.Password == "" {
		return errors.New("username and password are required")
	}
	return nil
}

func (a *App) createSession(w http.ResponseWriter, r *http.Request, username string) error {
	var userID int64
	if err := a.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE username=?`, username).Scan(&userID); err != nil {
		return err
	}
	return a.createSessionForUser(w, r, userID)
}

func (a *App) createSessionForUser(w http.ResponseWriter, r *http.Request, userID int64) error {
	secret, err := randomSecret()
	if err != nil {
		return err
	}
	csrf, err := randomSecret()
	if err != nil {
		return err
	}
	tokenHash := sha256.Sum256([]byte(secret))
	expires := time.Now().Add(sessionLifetime)
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix()); err != nil {
		return err
	}
	if _, err := a.db.ExecContext(r.Context(), `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES(?,?,?,?)`, tokenHash[:], userID, csrf, expires.Unix()); err != nil {
		return err
	}
	// A Secure cookie is dropped by the browser on plain-HTTP origins, which
	// would silently break login for a panel served without TLS. Send Secure
	// only when the client reaches us over HTTPS, either directly or through a
	// TLS-terminating proxy that forwards X-Forwarded-Proto.
	secure := a.secureCookies
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		secure = false
	}
	http.SetCookie(w, &http.Cookie{Name: "viya_session", Value: secret, Path: "/", Expires: expires, MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	return nil
}

func (a *App) me(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionContextKey{}).(sessionContext)
	writeJSON(w, http.StatusOK, map[string]string{
		"username":   session.username,
		"csrf_token": session.csrf,
		"role":       session.role,
	})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	secret, _ := sessionCookie(r)
	hash := sha256.Sum256([]byte(secret))
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash=?`, hash[:]); err != nil {
		fail(w, http.StatusInternalServerError, errors.New("could not end session"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "viya_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteStrictMode})
	if session, ok := r.Context().Value(sessionContextKey{}).(sessionContext); ok {
		a.recordLoginAudit(r, session.username, auditSuccess, auditReasonLogout)
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionContextKey struct{}
type sessionContext struct{ username, csrf, role string }

// Roles. A viewer may read everything but cannot change any configuration.
const (
	roleAdmin  = "admin"
	roleViewer = "viewer"
)

// viewerWritePaths are the only mutations a read-only account may perform:
// ending its own session and changing its own password.
var viewerWritePaths = map[string]bool{
	"/api/v1/auth/logout":   true,
	"/api/v1/auth/password": true,
}

func isViewerRole(role string) bool {
	return role == roleViewer
}

// isWriteRequest reports whether a method would change server state.
func isWriteRequest(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// requireAuth guards the panel API. Mutating requests also take the
// configuration read lock so a concurrent reload cannot interleave with an
// edit.
func (a *App) requireAuth(next http.Handler) http.Handler {
	return a.requireSession(next, true)
}

// requireAuthUnlocked is for handlers that acquire configMu themselves. Taking
// the read lock in the middleware would deadlock the configuration import, so
// the exemption is expressed on the route instead of by matching a URL string
// that would silently break if the route were renamed.
func (a *App) requireAuthUnlocked(next http.Handler) http.Handler {
	return a.requireSession(next, false)
}

func (a *App) requireSession(next http.Handler, holdConfigLock bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if holdConfigLock && r.Method != http.MethodGet {
			a.configMu.Lock()
			defer a.configMu.Unlock()
		}
		secret, ok := sessionCookie(r)
		if !ok {
			fail(w, http.StatusUnauthorized, errors.New("authentication required"))
			return
		}
		hash := sha256.Sum256([]byte(secret))
		var session sessionContext
		var expires int64
		if err := a.db.QueryRowContext(r.Context(), `SELECT users.username,sessions.csrf_token,sessions.expires_at,COALESCE(users.role,'admin') FROM sessions JOIN users ON users.id=sessions.user_id WHERE sessions.token_hash=?`, hash[:]).Scan(&session.username, &session.csrf, &expires, &session.role); err != nil || expires <= time.Now().Unix() {
			fail(w, http.StatusUnauthorized, errors.New("session expired; log in again"))
			return
		}
		if session.role != roleAdmin && session.role != roleViewer {
			// Unknown roles must fail closed to the least privilege, never
			// silently escalate to admin.
			slog.Warn("session has unknown role; downgrading to viewer", "role", session.role)
			session.role = roleViewer
		}
		if isViewerRole(session.role) && isWriteRequest(r.Method) && !viewerWritePaths[r.URL.Path] {
			fail(w, http.StatusForbidden, errors.New("this account is read-only"))
			return
		}
		a.renewSession(w, r, secret, expires)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !validOrigin(r, a.trustedProxies) {
				fail(w, http.StatusForbidden, errors.New("request origin rejected"))
				return
			}
			provided := r.Header.Get("X-CSRF-Token")
			if len(provided) != len(session.csrf) || subtle.ConstantTimeCompare([]byte(provided), []byte(session.csrf)) != 1 {
				fail(w, http.StatusForbidden, errors.New("CSRF token is missing or invalid"))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, session)))
	})
}

// renewSession slides the expiry forward once a session passes halfway, so an
// operator filling in a long form is not thrown back to the login screen at the
// 12 hour mark. Renewing only in the second half keeps the common request path
// free of extra writes.
func (a *App) renewSession(w http.ResponseWriter, r *http.Request, secret string, expires int64) {
	now := time.Now()
	if expires-now.Unix() > int64(sessionRenewAfter.Seconds()) {
		return
	}
	next := now.Add(sessionLifetime)
	hash := sha256.Sum256([]byte(secret))
	if _, err := a.db.ExecContext(r.Context(), `UPDATE sessions SET expires_at=? WHERE token_hash=?`, next.Unix(), hash[:]); err != nil {
		slog.Warn("renew session", "error", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "viya_session", Value: secret, Path: "/", Expires: next, MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteStrictMode})
}

func sessionCookie(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("viya_session")
	if err != nil || cookie.Value == "" || len(cookie.Value) > 128 {
		return "", false
	}
	return cookie.Value, true
}

func randomSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func validOrigin(r *http.Request, trustedProxies []*net.IPNet) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Scheme != requestScheme(r, trustedProxies) {
		return false
	}
	return true
}

func requestScheme(r *http.Request, trustedProxies []*net.IPNet) string {
	if r.TLS != nil {
		return "https"
	}
	remoteIP := net.ParseIP(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteIP = net.ParseIP(host)
	}
	if remoteIP == nil {
		return "http"
	}
	trusted := false
	for _, proxy := range trustedProxies {
		if proxy.Contains(remoteIP) {
			trusted = true
			break
		}
	}
	if !trusted {
		return "http"
	}
	for _, forwardedScheme := range strings.Split(r.Header.Get("X-Forwarded-Proto"), ",") {
		scheme := strings.TrimSpace(forwardedScheme)
		if scheme == "http" || scheme == "https" {
			return scheme
		}
	}
	return "http"
}

func parseTrustedProxies(value string) ([]*net.IPNet, error) {
	proxies := make([]*net.IPNet, 0)
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			if ip.To4() != nil {
				proxies = append(proxies, &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)})
			} else {
				proxies = append(proxies, &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
			}
			continue
		}
		_, proxy, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, errors.New("trusted proxies must be IP addresses or CIDR ranges")
		}
		proxies = append(proxies, proxy)
	}
	return proxies, nil
}

func (a *App) allowLogin(r *http.Request) bool {
	ip := remoteIP(r)
	now := time.Now().Unix()
	window := int64(loginWindowLength.Seconds())
	// A single atomic UPSERT keeps the rolling window consistent under
	// concurrent logins; a read-then-increment would let N requests all see
	// the same count and defeat the throttle.
	var count int
	err := a.db.QueryRowContext(r.Context(), `
		INSERT INTO login_attempts(ip,window_start,count) VALUES(?,?,1)
		ON CONFLICT(ip) DO UPDATE SET
			window_start = CASE WHEN login_attempts.window_start <= ? THEN excluded.window_start ELSE login_attempts.window_start END,
			count = CASE WHEN login_attempts.window_start <= ? THEN 1 ELSE login_attempts.count + 1 END
		RETURNING count`, ip, now, now-window, now-window).Scan(&count)
	if err != nil {
		// Refusing every login because the counter is unreadable would lock the
		// operator out of their own panel. Log it and let the password check run.
		slog.Warn("record login attempt", "error", err)
		return true
	}
	if count == 1 {
		if _, err := a.db.ExecContext(r.Context(), `DELETE FROM login_attempts WHERE window_start < ?`, now-3600); err != nil {
			slog.Warn("prune login attempts", "error", err)
		}
	}
	return count <= loginMaxAttempts
}

func (a *App) clearLoginAttempts(r *http.Request) {
	if _, err := a.db.ExecContext(r.Context(), `DELETE FROM login_attempts WHERE ip=?`, remoteIP(r)); err != nil {
		slog.Warn("clear login attempts", "error", err)
	}
}

func remoteIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// Package auth is the server's only source of truth about who a caller is
// and what they may do. The Angular client never supplies a role; every
// role comes from a session the server itself issued, looked up fresh on
// every request. Nothing in this package trusts a client-declared value.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

type Session struct {
	Token     string
	Username  string
	Role      string
	ExpiresAt time.Time
}

// demoUsers is the fixed, server-side account table. A client can never
// request a role; it can only authenticate as one of these fixed accounts,
// and the server looks up the role itself.
var demoUsers = map[string]string{
	"analyst1":    model.RoleAnalyst,
	"analyst2":    model.RoleAnalyst,
	"supervisor1": model.RoleSupervisor,
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
	audit    []AuditEntry
}

type AuditEntry struct {
	Time   time.Time `json:"time"`
	Event  string    `json:"event"`
	Detail string    `json:"detail"`
}

func NewStore() *Store {
	return &Store{sessions: make(map[string]*Session)}
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Login looks up the requested username in the fixed account table and
// mints a session for whatever role the table says, ignoring any role the
// caller may have also sent. Returns "", false for an unknown username.
func (s *Store) Login(username string) (string, bool) {
	role, ok := demoUsers[username]
	if !ok {
		return "", false
	}
	token := randomToken()
	s.mu.Lock()
	s.sessions[token] = &Session{Token: token, Username: username, Role: role, ExpiresAt: time.Now().Add(2 * time.Hour)}
	s.mu.Unlock()
	return token, true
}

func (s *Store) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// Lookup returns the session for a bearer token, or nil if it is missing,
// malformed, unknown, or expired. There is no other way into a Session.
func (s *Store) Lookup(authHeader string) *Session {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return nil
	}
	token := strings.TrimPrefix(authHeader, prefix)
	if token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return nil
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, token)
		return nil
	}
	return sess
}

// ForceExpire is used only by the bypass audit harness to construct an
// expired-session test case deterministically, by minting a session whose
// expiry is already in the past.
func (s *Store) ForceExpire(username, role string) string {
	token := randomToken()
	s.mu.Lock()
	s.sessions[token] = &Session{Token: token, Username: username, Role: role, ExpiresAt: time.Now().Add(-1 * time.Minute)}
	s.mu.Unlock()
	return token
}

func (s *Store) Audit(event, detail string) {
	s.mu.Lock()
	s.audit = append(s.audit, AuditEntry{Time: time.Now(), Event: event, Detail: detail})
	s.mu.Unlock()
}

func (s *Store) AuditLog() []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEntry, len(s.audit))
	copy(out, s.audit)
	return out
}

// CanAccessClassification reports whether a role may see a document at the
// given classification at all, regardless of what the caller asked for.
func CanAccessClassification(role, classification string) bool {
	if classification == model.ClassificationRestricted {
		return role == model.RoleSupervisor
	}
	return true
}

// allowedTransitions enumerates every legal forward move in the triage
// workflow and which role may make it. Anything not listed here, in either
// direction, is rejected. CLOSED has no outgoing edges: it is terminal.
var allowedTransitions = map[[2]string]string{
	{model.StatusNew, model.StatusInReview}:      model.RoleAnalyst,
	{model.StatusInReview, model.StatusFlagged}:  model.RoleAnalyst,
	{model.StatusFlagged, model.StatusClosed}:    model.RoleSupervisor,
}

// CanTransition reports whether role may move a document from `from` to
// `to`, and if not, a human-readable reason naming exactly what failed.
func CanTransition(role, from, to string) (bool, string) {
	requiredRole, exists := allowedTransitions[[2]string{from, to}]
	if !exists {
		return false, "no such transition: " + from + " -> " + to
	}
	if requiredRole == model.RoleSupervisor && role != model.RoleSupervisor {
		return false, "transition " + from + " -> " + to + " requires SUPERVISOR"
	}
	return true, ""
}

func WriteJSONError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + reason + `"}`))
}

// The Go REST service behind the search and triage console. The Angular
// client calls nothing but this API; every rule the client's UI enforces
// for usability (disabled buttons, hidden filters) is independently
// re-enforced here, because the client is not trusted.
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/Manas103/document-search-triage-console/backend/internal/auth"
	"github.com/Manas103/document-search-triage-console/backend/internal/es"
	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

type server struct {
	es    *es.Client
	auth  *auth.Store
}

func main() {
	addr := os.Getenv("ES_ADDR")
	if addr == "" {
		addr = "http://localhost:9200"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	client, err := es.NewClient(addr)
	if err != nil {
		log.Fatalf("elasticsearch client: %v", err)
	}
	s := &server{es: client, auth: auth.NewStore()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/session", s.handleLogin)
	mux.HandleFunc("POST /api/session/logout", s.handleLogout)
	mux.HandleFunc("GET /api/search", s.requireSession(s.handleSearch))
	mux.HandleFunc("GET /api/documents/{id}", s.requireSession(s.handleGetDocument))
	mux.HandleFunc("POST /api/documents/{id}/triage", s.requireSession(s.handleTriage))
	mux.HandleFunc("GET /api/audit-log", s.requireRole(model.RoleSupervisor, s.handleAuditLog))

	// Any other method on these paths is a deliberate 405, not a silent
	// fallthrough: DELETE /api/documents/{id} has no handler at all, for
	// example, because this service has no delete capability anywhere.
	mux.HandleFunc("/api/documents/{id}", s.methodNotAllowed)
	mux.HandleFunc("/api/search", s.methodNotAllowed)

	handler := withCORS(mux)

	log.Printf("document-search-triage-console backend listening on :%s (elasticsearch at %s)", port, addr)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	auth.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed on this resource")
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		// Role is deliberately accepted and deliberately ignored: this
		// field exists only so the bypass audit can prove that claiming a
		// role at login time does nothing.
		Role string `json:"role"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		auth.WriteJSONError(w, http.StatusBadRequest, "malformed login body")
		return
	}
	token, ok := s.auth.Login(body.Username)
	if !ok {
		s.auth.Audit("login_rejected", "unknown username: "+body.Username)
		auth.WriteJSONError(w, http.StatusUnauthorized, "unknown username")
		return
	}
	s.auth.Audit("login", body.Username)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token != "" {
		s.auth.Logout(token)
	}
	w.WriteHeader(http.StatusNoContent)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

type ctxKey int

const sessionKey ctxKey = 0

func (s *server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.auth.Lookup(r.Header.Get("Authorization"))
		if sess == nil {
			s.auth.Audit("auth_rejected", r.Method+" "+r.URL.Path+" no valid session")
			auth.WriteJSONError(w, http.StatusUnauthorized, "missing or invalid session")
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next(w, r.WithContext(ctx))
	}
}

func (s *server) requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		sess := r.Context().Value(sessionKey).(*auth.Session)
		if sess.Role != role {
			s.auth.Audit("role_rejected", r.Method+" "+r.URL.Path+" role="+sess.Role+" required="+role)
			auth.WriteJSONError(w, http.StatusForbidden, "requires role "+role)
			return
		}
		next(w, r)
	})
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	sess := r.Context().Value(sessionKey).(*auth.Session)
	q := model.Query{
		Text:           r.URL.Query().Get("q"),
		Category:       r.URL.Query().Get("category"),
		Region:         r.URL.Query().Get("region"),
		Priority:       r.URL.Query().Get("priority"),
		SourceType:     r.URL.Query().Get("source_type"),
		Classification: r.URL.Query().Get("classification"),
		Status:         r.URL.Query().Get("status"),
	}
	if size, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && size > 0 {
		q.Size = size
	}

	// Data-level authorization: a session that cannot see RESTRICTED
	// material cannot see it by asking for it either. If it explicitly
	// asked for a classification it may not have, that is rejected
	// outright rather than silently emptied, so the caller learns nothing
	// about whether matching restricted documents exist.
	if q.Classification != "" && !auth.CanAccessClassification(sess.Role, q.Classification) {
		s.auth.Audit("classification_search_rejected", sess.Username+" requested "+q.Classification)
		auth.WriteJSONError(w, http.StatusForbidden, "cannot search this classification")
		return
	}

	docs, total, err := s.es.Search(r.Context(), q)
	if err != nil {
		auth.WriteJSONError(w, http.StatusBadGateway, "search backend error")
		return
	}

	// Even when no classification filter was requested, results are
	// filtered again here against the caller's role. This is what makes
	// the restriction a property of the server, not of the query the
	// client happened to send.
	visible := make([]model.Document, 0, len(docs))
	for _, d := range docs {
		if auth.CanAccessClassification(sess.Role, d.Classification) {
			visible = append(visible, d)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"results": visible,
		"total":   total,
	})
}

func (s *server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	sess := r.Context().Value(sessionKey).(*auth.Session)
	id := r.PathValue("id")
	if !validDocID(id) {
		auth.WriteJSONError(w, http.StatusBadRequest, "invalid document id")
		return
	}
	doc, err := s.es.GetByID(r.Context(), id)
	if err != nil {
		auth.WriteJSONError(w, http.StatusBadGateway, "lookup backend error")
		return
	}
	if doc == nil {
		auth.WriteJSONError(w, http.StatusNotFound, "no such document")
		return
	}
	if !auth.CanAccessClassification(sess.Role, doc.Classification) {
		s.auth.Audit("classification_get_rejected", sess.Username+" fetched "+id+" ("+doc.Classification+")")
		auth.WriteJSONError(w, http.StatusForbidden, "cannot view this document")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

type triageRequest struct {
	Status string `json:"status"`
	// Classification and Role are accepted so a malicious client's attempt
	// to smuggle them is a parse success, not a parse error; they are read
	// by nothing below. The handler only ever uses Status.
	Classification string `json:"classification,omitempty"`
	Role           string `json:"role,omitempty"`
}

func validDocID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

func validStatus(status string) bool {
	switch status {
	case model.StatusNew, model.StatusInReview, model.StatusFlagged, model.StatusClosed:
		return true
	}
	return false
}

func (s *server) handleTriage(w http.ResponseWriter, r *http.Request) {
	sess := r.Context().Value(sessionKey).(*auth.Session)
	id := r.PathValue("id")
	if !validDocID(id) {
		s.auth.Audit("triage_rejected", "invalid id format: "+id)
		auth.WriteJSONError(w, http.StatusBadRequest, "invalid document id")
		return
	}

	ct := r.Header.Get("Content-Type")
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		s.auth.Audit("triage_rejected", "wrong content-type: "+ct)
		auth.WriteJSONError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil {
		auth.WriteJSONError(w, http.StatusBadRequest, "could not read body")
		return
	}
	if len(body) > 1<<20 {
		s.auth.Audit("triage_rejected", "oversized payload")
		auth.WriteJSONError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}

	var req triageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.auth.Audit("triage_rejected", "malformed json body")
		auth.WriteJSONError(w, http.StatusBadRequest, "malformed json body")
		return
	}
	if req.Status == "" {
		s.auth.Audit("triage_rejected", "missing status field")
		auth.WriteJSONError(w, http.StatusBadRequest, "status is required")
		return
	}
	if !validStatus(req.Status) {
		s.auth.Audit("triage_rejected", "invalid status value: "+req.Status)
		auth.WriteJSONError(w, http.StatusBadRequest, "invalid status value")
		return
	}

	doc, err := s.es.GetByID(r.Context(), id)
	if err != nil {
		auth.WriteJSONError(w, http.StatusBadGateway, "lookup backend error")
		return
	}
	if doc == nil {
		s.auth.Audit("triage_rejected", "no such document: "+id)
		auth.WriteJSONError(w, http.StatusNotFound, "no such document")
		return
	}

	if !auth.CanAccessClassification(sess.Role, doc.Classification) {
		s.auth.Audit("classification_triage_rejected", sess.Username+" on "+id+" ("+doc.Classification+")")
		auth.WriteJSONError(w, http.StatusForbidden, "cannot triage this document")
		return
	}

	ok, reason := auth.CanTransition(sess.Role, doc.Status, req.Status)
	if !ok {
		s.auth.Audit("transition_rejected", sess.Username+" on "+id+": "+reason)
		auth.WriteJSONError(w, http.StatusConflict, reason)
		return
	}

	if err := s.es.UpdateStatus(r.Context(), id, req.Status); err != nil {
		auth.WriteJSONError(w, http.StatusBadGateway, "update backend error")
		return
	}
	s.auth.Audit("triage_applied", sess.Username+" set "+id+" to "+req.Status)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": req.Status})
}

func (s *server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	entries := s.auth.AuditLog()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

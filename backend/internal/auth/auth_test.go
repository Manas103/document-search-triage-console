package auth

import (
	"testing"

	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

func TestLoginOnlyIssuesFixedRoles(t *testing.T) {
	s := NewStore()
	token, ok := s.Login("analyst1")
	if !ok || token == "" {
		t.Fatalf("expected analyst1 to log in")
	}
	sess := s.Lookup("Bearer " + token)
	if sess == nil || sess.Role != model.RoleAnalyst {
		t.Fatalf("expected ANALYST role, got %+v", sess)
	}

	_, ok = s.Login("nobody")
	if ok {
		t.Fatalf("expected unknown username to be rejected")
	}
}

func TestLookupRejectsMalformedHeaders(t *testing.T) {
	s := NewStore()
	token, _ := s.Login("analyst1")
	cases := []string{
		"",
		token,                  // missing "Bearer " prefix
		"Bearer ",              // empty token
		"Basic " + token,       // wrong scheme
		"Bearer wrong-token",   // never issued
		"Bearer  " + token + " ", // whitespace, must not be trimmed
	}
	for _, header := range cases {
		if sess := s.Lookup(header); sess != nil {
			t.Fatalf("expected header %q to be rejected, got session for %s", header, sess.Username)
		}
	}
}

func TestLogoutInvalidatesToken(t *testing.T) {
	s := NewStore()
	token, _ := s.Login("analyst1")
	s.Logout(token)
	if sess := s.Lookup("Bearer " + token); sess != nil {
		t.Fatalf("expected token to be invalid after logout")
	}
}

func TestCanAccessClassification(t *testing.T) {
	if !CanAccessClassification(model.RoleAnalyst, model.ClassificationUnclassified) {
		t.Error("ANALYST should see UNCLASSIFIED")
	}
	if !CanAccessClassification(model.RoleAnalyst, model.ClassificationCUI) {
		t.Error("ANALYST should see CUI")
	}
	if CanAccessClassification(model.RoleAnalyst, model.ClassificationRestricted) {
		t.Error("ANALYST should not see RESTRICTED")
	}
	if !CanAccessClassification(model.RoleSupervisor, model.ClassificationRestricted) {
		t.Error("SUPERVISOR should see RESTRICTED")
	}
}

func TestCanTransitionForwardOnly(t *testing.T) {
	cases := []struct {
		role, from, to string
		want           bool
	}{
		{model.RoleAnalyst, model.StatusNew, model.StatusInReview, true},
		{model.RoleAnalyst, model.StatusInReview, model.StatusFlagged, true},
		{model.RoleAnalyst, model.StatusFlagged, model.StatusClosed, false},
		{model.RoleSupervisor, model.StatusFlagged, model.StatusClosed, true},
		{model.RoleSupervisor, model.StatusClosed, model.StatusInReview, false},
		{model.RoleSupervisor, model.StatusNew, model.StatusClosed, false},
		{model.RoleAnalyst, model.StatusInReview, model.StatusInReview, false},
		{model.RoleAnalyst, model.StatusFlagged, model.StatusNew, false},
	}
	for _, c := range cases {
		got, reason := CanTransition(c.role, c.from, c.to)
		if got != c.want {
			t.Errorf("CanTransition(%s, %s -> %s) = %v (%s), want %v", c.role, c.from, c.to, got, reason, c.want)
		}
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	s := NewStore()
	token := s.ForceExpire("analyst1", model.RoleAnalyst)
	if sess := s.Lookup("Bearer " + token); sess != nil {
		t.Fatalf("expected an already-expired session to be rejected")
	}
}

package main

import (
	"testing"

	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

func TestMatchesAllFacets(t *testing.T) {
	d := model.Document{
		Title: "Cyber report near a facility", Body: "A convoy was observed.",
		Category: "Cyber", Region: "REGION-01", Priority: "HIGH",
		SourceType: "SENSOR", Classification: "CUI", Status: "NEW",
	}
	q := model.Query{Category: "Cyber", Region: "REGION-01", Priority: "HIGH", SourceType: "SENSOR", Classification: "CUI", Status: "NEW"}
	if !matches(q, d) {
		t.Fatalf("expected exact-facet document to match")
	}
	q.Status = "CLOSED"
	if matches(q, d) {
		t.Fatalf("expected mismatched status to fail")
	}
}

func TestMatchesTextIsCaseInsensitiveOverTitleAndBody(t *testing.T) {
	d := model.Document{Title: "Maritime report", Body: "A Vessel was observed departing REGION-02."}
	if !matches(model.Query{Text: "vessel"}, d) {
		t.Fatalf("expected case-insensitive body match")
	}
	if !matches(model.Query{Text: "MARITIME"}, d) {
		t.Fatalf("expected case-insensitive title match")
	}
	if matches(model.Query{Text: "pipeline"}, d) {
		t.Fatalf("expected non-matching text to fail")
	}
}

func TestEqualIDs(t *testing.T) {
	if !equalIDs([]string{"a", "b"}, []string{"a", "b"}) {
		t.Fatalf("expected identical sorted slices to be equal")
	}
	if equalIDs([]string{"a"}, []string{"a", "b"}) {
		t.Fatalf("expected different-length slices to be unequal")
	}
	if equalIDs([]string{"a", "c"}, []string{"a", "b"}) {
		t.Fatalf("expected differing elements to be unequal")
	}
}

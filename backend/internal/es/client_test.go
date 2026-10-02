package es

import (
	"testing"

	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

func TestBuildQueryAddsOnlyNonEmptyFilters(t *testing.T) {
	q := buildQuery(model.Query{Category: "Cyber", Region: "REGION-01"})
	boolQ := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	filters := boolQ["filter"].([]map[string]interface{})
	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d: %+v", len(filters), filters)
	}
}

func TestBuildQueryUsesMatchAllWithNoText(t *testing.T) {
	q := buildQuery(model.Query{Category: "Cyber"})
	boolQ := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	must := boolQ["must"].([]map[string]interface{})
	if _, ok := must[0]["match_all"]; !ok {
		t.Fatalf("expected match_all when no free text is given, got %+v", must[0])
	}
}

func TestBuildQueryUsesMultiMatchWithText(t *testing.T) {
	q := buildQuery(model.Query{Text: "convoy"})
	boolQ := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	must := boolQ["must"].([]map[string]interface{})
	mm, ok := must[0]["multi_match"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected multi_match when free text is given, got %+v", must[0])
	}
	if mm["query"] != "convoy" {
		t.Fatalf("expected query text to be preserved, got %+v", mm)
	}
}

func TestBuildQueryNoFiltersMeansEmptySlice(t *testing.T) {
	q := buildQuery(model.Query{})
	boolQ := q["query"].(map[string]interface{})["bool"].(map[string]interface{})
	filters := boolQ["filter"].([]map[string]interface{})
	if len(filters) != 0 {
		t.Fatalf("expected no filters for an empty query, got %+v", filters)
	}
}

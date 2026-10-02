// Package model defines the one document shape shared by the indexed query
// path and the brute-force oracle. Sharing the data shape is normal (every
// portfolio precedent does this); sharing filtering logic is not, and
// nothing in this file filters anything.
package model

// Document is one synthetic analyst report.
type Document struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Category       string `json:"category"`
	Region         string `json:"region"`
	Priority       string `json:"priority"`
	SourceType     string `json:"source_type"`
	Classification string `json:"classification"`
	Status         string `json:"status"`
	CreatedDate    string `json:"created_date"`
}

const (
	ClassificationUnclassified = "UNCLASSIFIED"
	ClassificationCUI          = "CUI"
	ClassificationRestricted   = "RESTRICTED"
)

const (
	StatusNew       = "NEW"
	StatusInReview  = "IN_REVIEW"
	StatusFlagged   = "FLAGGED"
	StatusClosed    = "CLOSED"
)

const (
	RoleAnalyst    = "ANALYST"
	RoleSupervisor = "SUPERVISOR"
)

// Query is the one shape both the ES query builder and the oracle predicate
// translate independently. Translating the same shape two different ways
// is the point; this struct carries no filtering behavior itself.
type Query struct {
	Text           string
	Category       string
	Region         string
	Priority       string
	SourceType     string
	Classification string
	Status         string
	Size           int
}

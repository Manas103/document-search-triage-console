// The bypass audit: 40 independently constructed direct-API calls, each
// written as if the Angular client did not exist, across 8 categories.
// Every one must be rejected (or, for the two data-level cases, must fail
// to leak the data it asked for) by the server itself. usage:
//   bypassaudit <api_base>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type apiResponse struct {
	status int
	body   []byte
}

func call(method, url, token, contentType string, body []byte) (apiResponse, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return apiResponse{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiResponse{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return apiResponse{status: resp.StatusCode, body: b}, nil
}

func login(apiBase, username string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"username": username})
	resp, err := call(http.MethodPost, apiBase+"/api/session", "", "application/json", payload)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.body, &parsed); err != nil {
		return "", fmt.Errorf("login failed: %s", string(resp.body))
	}
	return parsed.Token, nil
}

type searchResult struct {
	Results []struct {
		ID             string `json:"id"`
		Classification string `json:"classification"`
		Status         string `json:"status"`
	} `json:"results"`
}

func findDoc(apiBase, token, classification, status string) (string, error) {
	url := fmt.Sprintf("%s/api/search?classification=%s&status=%s&size=1", apiBase, classification, status)
	resp, err := call(http.MethodGet, url, "Bearer "+token, "", nil)
	if err != nil {
		return "", err
	}
	var parsed searchResult
	if err := json.Unmarshal(resp.body, &parsed); err != nil || len(parsed.Results) == 0 {
		return "", fmt.Errorf("no doc found for classification=%s status=%s (status=%d body=%s)", classification, status, resp.status, string(resp.body))
	}
	return parsed.Results[0].ID, nil
}

type bypassCase struct {
	category string
	name     string
	run      func() (pass bool, detail string)
}

func expectStatus(resp apiResponse, err error, wanted int) (bool, string) {
	if err != nil {
		return false, "request error: " + err.Error()
	}
	if resp.status != wanted {
		return false, fmt.Sprintf("expected HTTP %d, got %d, body=%s", wanted, resp.status, string(resp.body))
	}
	return true, fmt.Sprintf("HTTP %d as expected", resp.status)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: bypassaudit <api_base>")
		os.Exit(1)
	}
	apiBase := os.Args[1]

	analystToken, err := login(apiBase, "analyst1")
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: analyst login failed:", err)
		os.Exit(1)
	}
	supervisorToken, err := login(apiBase, "supervisor1")
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: supervisor login failed:", err)
		os.Exit(1)
	}

	restrictedID, err := findDoc(apiBase, supervisorToken, "RESTRICTED", "FLAGGED")
	if err != nil {
		restrictedID, err = findDoc(apiBase, supervisorToken, "RESTRICTED", "NEW")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: could not find a RESTRICTED doc:", err)
		os.Exit(1)
	}
	newID, err := findDoc(apiBase, supervisorToken, "UNCLASSIFIED", "NEW")
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: could not find a NEW/UNCLASSIFIED doc:", err)
		os.Exit(1)
	}
	inReviewID, err := findDoc(apiBase, supervisorToken, "UNCLASSIFIED", "IN_REVIEW")
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: could not find an IN_REVIEW/UNCLASSIFIED doc:", err)
		os.Exit(1)
	}
	flaggedID, err := findDoc(apiBase, supervisorToken, "UNCLASSIFIED", "FLAGGED")
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: could not find a FLAGGED/UNCLASSIFIED doc:", err)
		os.Exit(1)
	}
	closedID, err := findDoc(apiBase, supervisorToken, "UNCLASSIFIED", "CLOSED")
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: could not find a CLOSED/UNCLASSIFIED doc:", err)
		os.Exit(1)
	}
	const nonexistentID = "doc-99999999"

	bearerAnalyst := "Bearer " + analystToken
	bearerSupervisor := "Bearer " + supervisorToken

	triagePayload := func(status string) []byte {
		b, _ := json.Marshal(map[string]string{"status": status})
		return b
	}

	var cases []bypassCase

	add := func(category, name string, run func() (bool, string)) {
		cases = append(cases, bypassCase{category: category, name: name, run: run})
	}

	// A: missing / garbage / wrong-scheme auth
	add("A-auth", "no Authorization header on audit log", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/audit-log", "", "", nil)
		return expectStatus(r, err, 401)
	})
	add("A-auth", "empty bearer token on get-document", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/documents/"+newID, "Bearer ", "", nil)
		return expectStatus(r, err, 401)
	})
	add("A-auth", "garbage token on triage", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", "Bearer not-a-real-token", "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 401)
	})
	add("A-auth", "wrong auth scheme (Basic) on search", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search", "Basic dXNlcjpwYXNz", "", nil)
		return expectStatus(r, err, 401)
	})
	add("A-auth", "well-formed but never-issued token on audit log", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/audit-log", "Bearer "+strings.Repeat("ab", 24), "", nil)
		return expectStatus(r, err, 401)
	})

	// B: role-insufficient for protected actions
	add("B-role", "ANALYST attempts FLAGGED to CLOSED transition", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+flaggedID+"/triage", bearerAnalyst, "application/json", triagePayload("CLOSED"))
		return expectStatus(r, err, 409)
	})
	add("B-role", "ANALYST attempts to triage a RESTRICTED document", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+restrictedID+"/triage", bearerAnalyst, "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 403)
	})
	add("B-role", "ANALYST attempts GET /api/audit-log", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/audit-log", bearerAnalyst, "", nil)
		return expectStatus(r, err, 403)
	})
	add("B-role", "ANALYST attempts GET on a RESTRICTED document by id", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/documents/"+restrictedID, bearerAnalyst, "", nil)
		return expectStatus(r, err, 403)
	})
	add("B-role", "ANALYST attempts search with classification=RESTRICTED", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search?classification=RESTRICTED", bearerAnalyst, "", nil)
		return expectStatus(r, err, 403)
	})

	// C: client-supplied privilege/data spoofing ignored
	add("C-spoof", "X-Role: SUPERVISOR header ignored on a SUPERVISOR-only transition", func() (bool, string) {
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/api/documents/"+flaggedID+"/triage", bytes.NewReader(triagePayload("CLOSED")))
		req.Header.Set("Authorization", bearerAnalyst)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Role", "SUPERVISOR")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		return expectStatus(apiResponse{status: resp.StatusCode}, nil, 409)
	})
	add("C-spoof", "role field inside triage body ignored", func() (bool, string) {
		b, _ := json.Marshal(map[string]string{"status": "CLOSED", "role": "SUPERVISOR"})
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+flaggedID+"/triage", bearerAnalyst, "application/json", b)
		return expectStatus(r, err, 409)
	})
	add("C-spoof", "classification field inside triage body cannot bypass the classification gate", func() (bool, string) {
		b, _ := json.Marshal(map[string]string{"status": "IN_REVIEW", "classification": "UNCLASSIFIED"})
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+restrictedID+"/triage", bearerAnalyst, "application/json", b)
		return expectStatus(r, err, 403)
	})
	add("C-spoof", "login-time role claim is ignored, issued token stays ANALYST-privileged", func() (bool, string) {
		b, _ := json.Marshal(map[string]string{"username": "analyst1", "role": "SUPERVISOR"})
		loginResp, err := call(http.MethodPost, apiBase+"/api/session", "", "application/json", b)
		if err != nil || loginResp.status != 200 {
			return false, "login call itself failed unexpectedly"
		}
		var parsed struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(loginResp.body, &parsed)
		r, err := call(http.MethodGet, apiBase+"/api/audit-log", "Bearer "+parsed.Token, "", nil)
		return expectStatus(r, err, 403)
	})
	add("C-spoof", "role as query parameter ignored on audit log", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/audit-log?role=SUPERVISOR", bearerAnalyst, "", nil)
		return expectStatus(r, err, 403)
	})

	// D: malformed/invalid input rejected
	add("D-input", "malformed JSON body on triage", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", bearerAnalyst, "application/json", []byte("{not json"))
		return expectStatus(r, err, 400)
	})
	add("D-input", "missing required status field", func() (bool, string) {
		b, _ := json.Marshal(map[string]string{})
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", bearerAnalyst, "application/json", b)
		return expectStatus(r, err, 400)
	})
	add("D-input", "invalid status enum value", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", bearerAnalyst, "application/json", triagePayload("DELETED"))
		return expectStatus(r, err, 400)
	})
	add("D-input", "path-traversal-style document id", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/documents/..%2F..%2Fetc%2Fpasswd", bearerAnalyst, "", nil)
		return expectStatus(r, err, 400)
	})
	add("D-input", "oversized triage payload rejected", func() (bool, string) {
		big := make([]byte, 2<<20)
		for i := range big {
			big[i] = 'a'
		}
		b, _ := json.Marshal(map[string]string{"status": "IN_REVIEW", "padding": string(big)})
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", bearerAnalyst, "application/json", b)
		return expectStatus(r, err, 413)
	})

	// E: invalid workflow state transitions
	add("E-workflow", "NEW to CLOSED direct skip rejected", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", bearerSupervisor, "application/json", triagePayload("CLOSED"))
		return expectStatus(r, err, 409)
	})
	add("E-workflow", "CLOSED is terminal, reopen rejected", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+closedID+"/triage", bearerSupervisor, "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 409)
	})
	add("E-workflow", "FLAGGED to NEW backward transition rejected", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+flaggedID+"/triage", bearerSupervisor, "application/json", triagePayload("NEW"))
		return expectStatus(r, err, 409)
	})
	add("E-workflow", "triage on a nonexistent document id", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+nonexistentID+"/triage", bearerSupervisor, "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 404)
	})
	add("E-workflow", "IN_REVIEW to IN_REVIEW no-op rejected", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+inReviewID+"/triage", bearerAnalyst, "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 409)
	})

	// F: protocol/method-level rejections
	add("F-protocol", "wrong Content-Type on triage", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", bearerAnalyst, "text/plain", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 415)
	})
	add("F-protocol", "DELETE method on documents resource", func() (bool, string) {
		r, err := call(http.MethodDelete, apiBase+"/api/documents/"+newID, bearerSupervisor, "", nil)
		return expectStatus(r, err, 405)
	})
	add("F-protocol", "PUT method on search resource", func() (bool, string) {
		r, err := call(http.MethodPut, apiBase+"/api/search", bearerSupervisor, "", nil)
		return expectStatus(r, err, 405)
	})
	add("F-protocol", "PATCH method on triage resource", func() (bool, string) {
		r, err := call(http.MethodPatch, apiBase+"/api/documents/"+newID+"/triage", bearerSupervisor, "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 405)
	})
	add("F-protocol", "GET method on triage resource", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/documents/"+newID+"/triage", bearerSupervisor, "", nil)
		return expectStatus(r, err, 405)
	})

	// G: session token lifecycle misuse
	add("G-session", "token rejected after explicit logout", func() (bool, string) {
		token, err := login(apiBase, "analyst2")
		if err != nil {
			return false, "setup login failed: " + err.Error()
		}
		_, _ = call(http.MethodPost, apiBase+"/api/session/logout", "Bearer "+token, "", nil)
		r, err := call(http.MethodGet, apiBase+"/api/search", "Bearer "+token, "", nil)
		return expectStatus(r, err, 401)
	})
	add("G-session", "token with one character corrupted", func() (bool, string) {
		corrupted := []byte(analystToken)
		if corrupted[0] == 'a' {
			corrupted[0] = 'b'
		} else {
			corrupted[0] = 'a'
		}
		r, err := call(http.MethodGet, apiBase+"/api/search", "Bearer "+string(corrupted), "", nil)
		return expectStatus(r, err, 401)
	})
	add("G-session", "token with surrounding whitespace rejected (no trimming)", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search", "Bearer  "+analystToken+" ", "", nil)
		return expectStatus(r, err, 401)
	})
	add("G-session", "Bearer prefix missing entirely", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search", analystToken, "", nil)
		return expectStatus(r, err, 401)
	})
	add("G-session", "random 48-hex-char token never issued", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search", "Bearer "+strings.Repeat("f0", 24), "", nil)
		return expectStatus(r, err, 401)
	})

	// H: data-level authorization beyond a simple role check
	add("H-data", "ANALYST search results never include RESTRICTED documents", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search?size=500", bearerAnalyst, "", nil)
		if err != nil || r.status != 200 {
			return false, fmt.Sprintf("search itself failed: status=%d err=%v", r.status, err)
		}
		var parsed searchResult
		if err := json.Unmarshal(r.body, &parsed); err != nil {
			return false, "could not parse search response"
		}
		for _, d := range parsed.Results {
			if d.Classification == "RESTRICTED" {
				return false, "RESTRICTED document " + d.ID + " leaked into an ANALYST result set"
			}
		}
		return true, fmt.Sprintf("0 of %d results were RESTRICTED", len(parsed.Results))
	})
	add("H-data", "ANALYST direct GET on a RESTRICTED id obtained independently", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/documents/"+restrictedID, bearerAnalyst, "", nil)
		return expectStatus(r, err, 403)
	})
	add("H-data", "completely unauthenticated search", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/search", "", "", nil)
		return expectStatus(r, err, 401)
	})
	add("H-data", "completely unauthenticated document fetch", func() (bool, string) {
		r, err := call(http.MethodGet, apiBase+"/api/documents/"+newID, "", "", nil)
		return expectStatus(r, err, 401)
	})
	add("H-data", "completely unauthenticated triage attempt", func() (bool, string) {
		r, err := call(http.MethodPost, apiBase+"/api/documents/"+newID+"/triage", "", "application/json", triagePayload("IN_REVIEW"))
		return expectStatus(r, err, 401)
	})

	if len(cases) != 40 {
		fmt.Fprintf(os.Stderr, "internal error: expected exactly 40 cases, built %d\n", len(cases))
		os.Exit(1)
	}

	passed := 0
	for i, c := range cases {
		ok, detail := c.run()
		status := "CAUGHT"
		if !ok {
			status = "MISSED"
		} else {
			passed++
		}
		fmt.Printf("[bypass %02d/40] [%s] %-70s %s (%s)\n", i+1, c.category, c.name, status, detail)
	}
	fmt.Printf("[bypass] DONE %d/%d caught\n", passed, len(cases))
	if passed != len(cases) {
		os.Exit(1)
	}
}

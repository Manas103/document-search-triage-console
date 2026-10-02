// The independent correctness and latency oracle. It never imports
// internal/es: it reads the raw document file directly, line by line, with
// its own hand-written predicate, so a bug in the Elasticsearch query
// builder is not mirrored here by construction.
//
// Two modes:
//   oracle scanlatency <file> <iterations>
//     Measures how long a genuine, unindexed, no-cache full scan of the
//     corpus takes, as the comparison baseline for the indexed path.
//   oracle correctness <file> <api_base> <session_token> <n>
//     Runs n randomized narrow queries against both this scanner and the
//     real running API, and reports exact matches vs mismatches.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

func matches(q model.Query, d model.Document) bool {
	if q.Category != "" && d.Category != q.Category {
		return false
	}
	if q.Region != "" && d.Region != q.Region {
		return false
	}
	if q.Priority != "" && d.Priority != q.Priority {
		return false
	}
	if q.SourceType != "" && d.SourceType != q.SourceType {
		return false
	}
	if q.Classification != "" && d.Classification != q.Classification {
		return false
	}
	if q.Status != "" && d.Status != q.Status {
		return false
	}
	if q.Text != "" {
		needle := strings.ToLower(q.Text)
		if !strings.Contains(strings.ToLower(d.Title), needle) && !strings.Contains(strings.ToLower(d.Body), needle) {
			return false
		}
	}
	return true
}

// scanFile performs one full, uncached pass over the corpus file, applying
// the predicate above. It opens and re-reads the file from disk every call;
// nothing about a prior call is reused by a later one.
func scanFile(path string, q model.Query) ([]model.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var results []model.Document
	for scanner.Scan() {
		var d model.Document
		if err := json.Unmarshal(scanner.Bytes(), &d); err != nil {
			continue
		}
		if matches(q, d) {
			results = append(results, d)
		}
	}
	return results, scanner.Err()
}

var (
	categories      = []string{"Infrastructure", "Maritime", "Aviation", "Cyber", "Logistics", "Communications", "Border", "Industrial"}
	regions         = func() []string { r := make([]string, 12); for i := range r { r[i] = fmt.Sprintf("REGION-%02d", i+1) }; return r }()
	priorities      = []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"}
	sourceTypes     = []string{"SENSOR", "FIELD", "OPEN_SOURCE", "PARTNER"}
	classifications = []string{"UNCLASSIFIED", "CUI", "RESTRICTED"}
	statuses        = []string{"NEW", "IN_REVIEW", "FLAGGED", "CLOSED"}
)

// narrowQuery sets every facet so the matching bucket is small enough that
// the indexed path's result cap never silently truncates the comparison.
func narrowQuery(rng *rand.Rand) model.Query {
	return model.Query{
		Category:       categories[rng.Intn(len(categories))],
		Region:         regions[rng.Intn(len(regions))],
		Priority:       priorities[rng.Intn(len(priorities))],
		SourceType:     sourceTypes[rng.Intn(len(sourceTypes))],
		Classification: classifications[rng.Intn(len(classifications))],
		Status:         statuses[rng.Intn(len(statuses))],
	}
}

func idSet(docs []model.Document) []string {
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	sort.Strings(ids)
	return ids
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func runScanLatency(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: oracle scanlatency <file> <iterations>")
		os.Exit(1)
	}
	path := args[0]
	iterations, _ := strconv.Atoi(args[1])
	rng := rand.New(rand.NewSource(20260930))
	durations := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		q := narrowQuery(rng)
		start := time.Now()
		results, err := scanFile(path, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
			os.Exit(1)
		}
		elapsed := time.Since(start)
		durations = append(durations, elapsed)
		fmt.Printf("[scanlatency] iteration=%d matched=%d elapsed=%s\n", i+1, len(results), elapsed)
	}
	var total time.Duration
	min, max := durations[0], durations[0]
	for _, d := range durations {
		total += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	mean := total / time.Duration(len(durations))
	fmt.Printf("[scanlatency] DONE n=%d mean=%s min=%s max=%s\n", len(durations), mean, min, max)
}

type searchResponse struct {
	Results []model.Document `json:"results"`
	Total   int64            `json:"total"`
}

// size=500 is deliberate: it is the server's own maximum (internal/es.
// Client.Search clamps anything above 500 back down to the 200 default),
// not an arbitrary large number. The first version of this tool requested
// size=1000, which silently fell back to 200 and produced false mismatches
// whenever a narrow bucket had 201 to 500 real matches; see the README.
func apiSearch(apiBase, token string, q model.Query) ([]model.Document, error) {
	url := fmt.Sprintf("%s/api/search?category=%s&region=%s&priority=%s&source_type=%s&classification=%s&status=%s&size=500",
		apiBase, q.Category, q.Region, q.Priority, q.SourceType, q.Classification, q.Status)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("api search failed (%d): %s", resp.StatusCode, string(body))
	}
	var parsed searchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed.Results, nil
}

func runCorrectness(args []string) {
	if len(args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: oracle correctness <file> <api_base> <session_token> <n>")
		os.Exit(1)
	}
	path, apiBase, token := args[0], args[1], args[2]
	n, _ := strconv.Atoi(args[3])
	rng := rand.New(rand.NewSource(20261001))

	matched, mismatched := 0, 0
	start := time.Now()
	for i := 0; i < n; i++ {
		q := narrowQuery(rng)
		oracleDocs, err := scanFile(path, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
			os.Exit(1)
		}
		apiDocs, err := apiSearch(apiBase, token, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "api error: %v\n", err)
			os.Exit(1)
		}
		oracleIDs := idSet(oracleDocs)
		apiIDs := idSet(apiDocs)
		if equalIDs(oracleIDs, apiIDs) {
			matched++
		} else {
			mismatched++
			fmt.Printf("[correctness] MISMATCH query=%+v oracle_n=%d api_n=%d\n", q, len(oracleIDs), len(apiIDs))
		}
		if (i+1)%100 == 0 {
			fmt.Printf("[correctness] %d/%d done, matched=%d mismatched=%d, %.1fs elapsed\n", i+1, n, matched, mismatched, time.Since(start).Seconds())
		}
	}
	fmt.Printf("[correctness] DONE %d/%d matched, %d mismatched, elapsed=%.1fs\n", matched, n, mismatched, time.Since(start).Seconds())
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: oracle <scanlatency|correctness> ...")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "scanlatency":
		runScanLatency(os.Args[2:])
	case "correctness":
		runCorrectness(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "unknown mode:", os.Args[1])
		os.Exit(1)
	}
}

// Measures p95 latency of the indexed path through the real, running HTTP
// API (not the Elasticsearch driver directly), with a realistic interactive
// query shape: one to three facets, optionally free text, capped results.
// usage: bench <api_base> <session_token> <n>
package main

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"
)

var (
	categories  = []string{"Infrastructure", "Maritime", "Aviation", "Cyber", "Logistics", "Communications", "Border", "Industrial"}
	regions     = func() []string { r := make([]string, 12); for i := range r { r[i] = fmt.Sprintf("REGION-%02d", i+1) }; return r }()
	priorities  = []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"}
	sourceTypes = []string{"SENSOR", "FIELD", "OPEN_SOURCE", "PARTNER"}
	statuses    = []string{"NEW", "IN_REVIEW", "FLAGGED", "CLOSED"}
	textTerms   = []string{"convoy", "vessel", "facility", "checkpoint", "pipeline", "outage", "anomaly", "inspection"}
)

func realisticQueryParams(rng *rand.Rand) string {
	params := "size=200"
	numFacets := 1 + rng.Intn(3)
	choices := []func() string{
		func() string { return "category=" + categories[rng.Intn(len(categories))] },
		func() string { return "region=" + regions[rng.Intn(len(regions))] },
		func() string { return "priority=" + priorities[rng.Intn(len(priorities))] },
		func() string { return "source_type=" + sourceTypes[rng.Intn(len(sourceTypes))] },
		func() string { return "status=" + statuses[rng.Intn(len(statuses))] },
	}
	rng.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
	for i := 0; i < numFacets && i < len(choices); i++ {
		params += "&" + choices[i]()
	}
	if rng.Intn(2) == 0 {
		params += "&q=" + textTerms[rng.Intn(len(textTerms))]
	}
	return params
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: bench <api_base> <session_token> <n>")
		os.Exit(1)
	}
	apiBase, token := os.Args[1], os.Args[2]
	n, _ := strconv.Atoi(os.Args[3])
	rng := rand.New(rand.NewSource(20261002))

	// Warm-up requests are excluded from the measured distribution, as is
	// standard, so the first JIT-free Go binary call and first ES socket
	// setup are not counted as representative latency.
	for i := 0; i < 20; i++ {
		doRequest(apiBase, token, realisticQueryParams(rng))
	}

	durations := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		d, err := doRequest(apiBase, token, realisticQueryParams(rng))
		if err != nil {
			fmt.Fprintf(os.Stderr, "request %d failed: %v\n", i, err)
			os.Exit(1)
		}
		durations = append(durations, d)
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	mean := total / time.Duration(len(durations))
	p50 := durations[len(durations)*50/100]
	p95 := durations[len(durations)*95/100]
	max := durations[len(durations)-1]
	fmt.Printf("[bench] INDEXED LATENCY n=%d mean=%s p50=%s p95=%s max=%s\n", n, mean, p50, p95, max)
}

func doRequest(apiBase, token, params string) (time.Duration, error) {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/api/search?"+params, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	elapsed := time.Since(start)
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	return elapsed, nil
}

// Reads the synthetic NDJSON corpus and bulk-indexes it into Elasticsearch.
// Run once per fresh Elasticsearch instance: go run ./cmd/indexer <file> <es_addr>
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Manas103/document-search-triage-console/backend/internal/es"
	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: indexer <documents.ndjson> <es_addr>")
		os.Exit(1)
	}
	path := os.Args[1]
	addr := os.Args[2]

	client, err := es.NewClient(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		if err := client.Ping(ctx); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err := client.EnsureIndex(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ensure index: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	const batchSize = 2000
	batch := make([]model.Document, 0, batchSize)
	start := time.Now()
	total := 0

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := client.BulkIndex(ctx, batch); err != nil {
			fmt.Fprintf(os.Stderr, "bulk index: %v\n", err)
			os.Exit(1)
		}
		total += len(batch)
		batch = batch[:0]
	}

	for scanner.Scan() {
		var d model.Document
		if err := json.Unmarshal(scanner.Bytes(), &d); err != nil {
			fmt.Fprintf(os.Stderr, "skip malformed line: %v\n", err)
			continue
		}
		batch = append(batch, d)
		if len(batch) >= batchSize {
			flush()
			if total%100000 == 0 {
				fmt.Printf("[index] %d documents, %.1fs elapsed\n", total, time.Since(start).Seconds())
			}
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
		os.Exit(1)
	}

	if err := client.Refresh(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "refresh: %v\n", err)
		os.Exit(1)
	}
	count, err := client.Count(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "count: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[index] DONE indexed=%d es_count=%d elapsed=%.1fs\n", total, count, time.Since(start).Seconds())
}

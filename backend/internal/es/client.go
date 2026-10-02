// Package es is the only place that talks to Elasticsearch. It builds the
// indexed query path; it never implements the brute-force comparison, which
// lives entirely in cmd/oracle and never imports this package's query
// builder.
package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"

	"github.com/Manas103/document-search-triage-console/backend/internal/model"
)

const IndexName = "reports"

const mapping = `{
  "settings": { "number_of_shards": 1, "number_of_replicas": 0 },
  "mappings": {
    "properties": {
      "id": { "type": "keyword" },
      "title": { "type": "text" },
      "body": { "type": "text" },
      "category": { "type": "keyword" },
      "region": { "type": "keyword" },
      "priority": { "type": "keyword" },
      "source_type": { "type": "keyword" },
      "classification": { "type": "keyword" },
      "status": { "type": "keyword" },
      "created_date": { "type": "date" }
    }
  }
}`

type Client struct {
	es *elasticsearch.Client
}

func NewClient(addr string) (*Client, error) {
	cfg := elasticsearch.Config{Addresses: []string{addr}}
	c, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{es: c}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	res, err := c.es.Ping(c.es.Ping.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("ping failed: %s", res.String())
	}
	return nil
}

// EnsureIndex creates the index with the mapping above if it does not
// already exist. Called once by the indexer, not by the server.
func (c *Client) EnsureIndex(ctx context.Context) error {
	existsRes, err := c.es.Indices.Exists([]string{IndexName}, c.es.Indices.Exists.WithContext(ctx))
	if err != nil {
		return err
	}
	defer existsRes.Body.Close()
	if existsRes.StatusCode == 200 {
		return nil
	}
	createRes, err := c.es.Indices.Create(IndexName,
		c.es.Indices.Create.WithContext(ctx),
		c.es.Indices.Create.WithBody(strings.NewReader(mapping)),
	)
	if err != nil {
		return err
	}
	defer createRes.Body.Close()
	if createRes.IsError() {
		return fmt.Errorf("create index failed: %s", createRes.String())
	}
	return nil
}

// BulkIndex indexes a batch of documents using the ES bulk API.
func (c *Client) BulkIndex(ctx context.Context, docs []model.Document) error {
	var buf bytes.Buffer
	for _, d := range docs {
		meta := map[string]map[string]string{"index": {"_index": IndexName, "_id": d.ID}}
		metaLine, _ := json.Marshal(meta)
		buf.Write(metaLine)
		buf.WriteByte('\n')
		docLine, _ := json.Marshal(d)
		buf.Write(docLine)
		buf.WriteByte('\n')
	}
	res, err := c.es.Bulk(bytes.NewReader(buf.Bytes()), c.es.Bulk.WithContext(ctx), c.es.Bulk.WithIndex(IndexName))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.IsError() {
		return fmt.Errorf("bulk failed: %s", string(body))
	}
	var parsed struct {
		Errors bool `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Errors {
		return fmt.Errorf("bulk reported per-item errors: %s", string(body))
	}
	return nil
}

func (c *Client) Refresh(ctx context.Context) error {
	res, err := c.es.Indices.Refresh(c.es.Indices.Refresh.WithContext(ctx), c.es.Indices.Refresh.WithIndex(IndexName))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (c *Client) Count(ctx context.Context) (int64, error) {
	res, err := c.es.Count(c.es.Count.WithContext(ctx), c.es.Count.WithIndex(IndexName))
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var parsed struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, err
	}
	return parsed.Count, nil
}

// buildQuery translates a model.Query into the Elasticsearch DSL. This is
// the indexed path's only translation of "what does the query mean"; the
// oracle in cmd/oracle has its own, written without reference to this
// function, which is what makes a diff between them meaningful.
func buildQuery(q model.Query) map[string]interface{} {
	var filters []map[string]interface{}
	addTerm := func(field, value string) {
		if value != "" {
			filters = append(filters, map[string]interface{}{"term": map[string]interface{}{field: value}})
		}
	}
	addTerm("category", q.Category)
	addTerm("region", q.Region)
	addTerm("priority", q.Priority)
	addTerm("source_type", q.SourceType)
	addTerm("classification", q.Classification)
	addTerm("status", q.Status)

	boolQuery := map[string]interface{}{"filter": filters}
	if q.Text != "" {
		boolQuery["must"] = []map[string]interface{}{
			{"multi_match": map[string]interface{}{"query": q.Text, "fields": []string{"title^2", "body"}}},
		}
	} else {
		boolQuery["must"] = []map[string]interface{}{{"match_all": map[string]interface{}{}}}
	}
	return map[string]interface{}{"query": map[string]interface{}{"bool": boolQuery}}
}

// Search runs the indexed path: an ES bool query with term filters riding
// the keyword-field indexes, optionally scored by a multi_match on the free
// text fields. Results are capped by q.Size, mirroring the API's own cap.
func (c *Client) Search(ctx context.Context, q model.Query) ([]model.Document, int64, error) {
	body := buildQuery(q)
	size := q.Size
	if size <= 0 || size > 500 {
		size = 200
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	res, err := c.es.Search(
		c.es.Search.WithContext(ctx),
		c.es.Search.WithIndex(IndexName),
		c.es.Search.WithBody(bytes.NewReader(payload)),
		c.es.Search.WithSize(size),
		c.es.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.IsError() {
		return nil, 0, fmt.Errorf("search failed: %s", string(raw))
	}
	var parsed struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source model.Document `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, 0, err
	}
	docs := make([]model.Document, 0, len(parsed.Hits.Hits))
	for _, h := range parsed.Hits.Hits {
		docs = append(docs, h.Source)
	}
	return docs, parsed.Hits.Total.Value, nil
}

// GetByID fetches a single document by id, or (nil, nil) if not found.
func (c *Client) GetByID(ctx context.Context, id string) (*model.Document, error) {
	res, err := c.es.Get(IndexName, id, c.es.Get.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, nil
	}
	raw, _ := io.ReadAll(res.Body)
	if res.IsError() {
		return nil, fmt.Errorf("get failed: %s", string(raw))
	}
	var parsed struct {
		Source model.Document `json:"_source"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return &parsed.Source, nil
}

// UpdateStatus updates only the status field of a document, used by the
// triage mutation path. It never touches classification: the triage
// endpoint cannot change a document's classification, by construction.
func (c *Client) UpdateStatus(ctx context.Context, id, status string) error {
	body := map[string]interface{}{"doc": map[string]string{"status": status}}
	payload, _ := json.Marshal(body)
	res, err := c.es.Update(IndexName, id, bytes.NewReader(payload), c.es.Update.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, _ := io.ReadAll(res.Body)
		return fmt.Errorf("update failed: %s", string(raw))
	}
	return nil
}

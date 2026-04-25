package couchdb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"gobsidian-cli/internal/plugins/livesync/protocol"
)

type Config struct {
	URL      string `json:"url"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Client struct {
	cfg        Config
	httpClient *resty.Client
}

type couchError struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

func New(cfg Config) *Client {
	client := resty.New().
		SetTimeout(30 * time.Second)
	if cfg.Username != "" || cfg.Password != "" {
		client.SetBasicAuth(cfg.Username, cfg.Password)
	}
	return &Client{cfg: cfg, httpClient: client}
}

type Change struct {
	ID      string
	Seq     string
	Deleted bool
	Record  protocol.Record
}

func (c *Client) Changes(ctx context.Context, since string) ([]Change, string, error) {
	values := url.Values{}
	values.Set("include_docs", "true")
	values.Set("style", "main_only")
	if since != "" {
		values.Set("since", since)
	}
	resp, err := c.httpClient.R().
		SetContext(ctx).
		SetQueryParamsFromValues(values).
		Get(c.endpoint("_changes"))
	if err != nil {
		return nil, "", err
	}
	if resp.IsError() {
		return nil, "", fmt.Errorf("fetch couchdb changes: %s: %s", resp.Status(), resp.String())
	}
	var out changesResponse
	if err := json.Unmarshal(resp.Body(), &out); err != nil {
		return nil, "", err
	}
	changes := make([]Change, 0, len(out.Results))
	for _, row := range out.Results {
		if skipDocID(row.ID) {
			continue
		}
		seq := rawSeqString(row.Seq)
		if row.Deleted {
			changes = append(changes, Change{ID: row.ID, Seq: seq, Deleted: true})
			continue
		}
		if len(row.Doc) == 0 {
			continue
		}
		record, err := decodeRecord(row.Doc)
		if err != nil {
			return nil, "", err
		}
		changes = append(changes, Change{ID: row.ID, Seq: seq, Deleted: false, Record: record})
	}
	return changes, rawSeqString(out.LastSeq), nil
}

func (c *Client) FetchRecords(ctx context.Context) ([]protocol.Record, error) {
	resp, err := c.httpClient.R().
		SetContext(ctx).
		SetQueryParam("include_docs", "true").
		Get(c.endpoint("_all_docs"))
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("fetch couchdb docs: %s: %s", resp.Status(), resp.String())
	}
	var out allDocsResponse
	if err := json.Unmarshal(resp.Body(), &out); err != nil {
		return nil, err
	}
	records := make([]protocol.Record, 0, len(out.Rows))
	for _, row := range out.Rows {
		if len(row.Doc) == 0 {
			continue
		}
		id, _ := row.Doc["_id"].(string)
		if skipDocID(id) {
			continue
		}
		record, err := decodeRecord(row.Doc)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (c *Client) FetchRecordsByID(ctx context.Context, ids []string) ([]protocol.Record, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	resp, err := c.httpClient.R().
		SetContext(ctx).
		SetQueryParam("include_docs", "true").
		SetBody(map[string]any{"keys": ids}).
		Post(c.endpoint("_all_docs"))
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("fetch couchdb docs by id: %s: %s", resp.Status(), resp.String())
	}
	var out allDocsResponse
	if err := json.Unmarshal(resp.Body(), &out); err != nil {
		return nil, err
	}
	records := make([]protocol.Record, 0, len(out.Rows))
	for _, row := range out.Rows {
		if len(row.Doc) == 0 {
			continue
		}
		id, _ := row.Doc["_id"].(string)
		if skipDocID(id) {
			continue
		}
		record, err := decodeRecord(row.Doc)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (c *Client) GetDoc(ctx context.Context, id string) (map[string]any, bool, error) {
	resp, err := c.httpClient.R().
		SetContext(ctx).
		Get(c.endpoint(c.escapeDocID(id)))
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode() == 404 {
		var couchErr couchError
		if err := json.Unmarshal(resp.Body(), &couchErr); err == nil && strings.Contains(strings.ToLower(couchErr.Reason), "database does not exist") {
			return nil, false, fmt.Errorf("couchdb database %q does not exist", c.cfg.Database)
		}
		return nil, false, nil
	}
	if resp.IsError() {
		return nil, false, fmt.Errorf("get couchdb doc %s: %s: %s", id, resp.Status(), resp.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(resp.Body(), &doc); err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

func (c *Client) SyncParameters(ctx context.Context) ([]byte, error) {
	doc, ok, err := c.GetDoc(ctx, "_local/obsidian_livesync_sync_parameters")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("LiveSync sync parameters are missing; sync Obsidian to this database once before enabling E2EE in gobsidian")
	}
	raw, _ := doc["pbkdf2salt"].(string)
	if raw == "" {
		return nil, fmt.Errorf("LiveSync sync parameters do not contain pbkdf2salt")
	}
	salt, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode LiveSync pbkdf2salt: %w", err)
	}
	return salt, nil
}

func (c *Client) BulkWrite(ctx context.Context, records []protocol.Record) (map[string]string, error) {
	reqBody := bulkDocsRequest{Docs: make([]map[string]any, 0, len(records))}
	revs := map[string]string{}
	chunkIDs := map[string]bool{}
	for _, record := range records {
		switch {
		case record.Chunk != nil:
			if chunkIDs[record.Chunk.ID] {
				continue
			}
			chunkIDs[record.Chunk.ID] = true
			if existing, ok, err := c.GetDoc(ctx, record.Chunk.ID); err != nil {
				return nil, err
			} else if ok {
				revs[record.Chunk.ID], _ = existing["_rev"].(string)
				continue
			}
			doc := map[string]any{
				"_id":  record.Chunk.ID,
				"type": "leaf",
				"data": record.Chunk.Data,
			}
			if record.Chunk.Encrypted {
				doc["e_"] = true
			}
			reqBody.Docs = append(reqBody.Docs, doc)
		case record.Document != nil:
			raw, err := json.Marshal(record.Document)
			if err != nil {
				return nil, err
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				return nil, err
			}
			delete(doc, "_revisions")
			if record.Document.Rev == "" {
				if existing, ok, err := c.GetDoc(ctx, record.Document.ID); err != nil {
					return nil, err
				} else if ok {
					doc["_rev"], _ = existing["_rev"].(string)
				}
			}
			reqBody.Docs = append(reqBody.Docs, doc)
		}
	}
	if len(reqBody.Docs) == 0 {
		return revs, nil
	}
	resp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		Post(c.endpoint("_bulk_docs"))
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("bulk write couchdb docs: %s: %s", resp.Status(), resp.String())
	}
	var results []bulkDocsResponse
	if err := json.Unmarshal(resp.Body(), &results); err != nil {
		return nil, err
	}
	for _, result := range results {
		if !result.OK {
			if result.Error == "conflict" && strings.HasPrefix(result.ID, "h:") {
				existing, ok, err := c.GetDoc(ctx, result.ID)
				if err != nil {
					return nil, err
				}
				typ, _ := existing["type"].(string)
				if ok && typ == "leaf" {
					revs[result.ID], _ = existing["_rev"].(string)
					continue
				}
			}
			return nil, fmt.Errorf("bulk write failed for %s: %s %s", result.ID, result.Error, result.Reason)
		}
		revs[result.ID] = result.Rev
	}
	return revs, nil
}

func (c *Client) endpoint(path string) string {
	base := strings.TrimRight(c.cfg.URL, "/")
	db := url.PathEscape(c.cfg.Database)
	return base + "/" + db + "/" + strings.TrimLeft(path, "/")
}

func (c *Client) escapeDocID(id string) string {
	if strings.HasPrefix(id, "_local/") {
		return "_local/" + url.PathEscape(strings.TrimPrefix(id, "_local/"))
	}
	return url.PathEscape(id)
}

type allDocsResponse struct {
	Rows []struct {
		Doc map[string]any `json:"doc"`
	} `json:"rows"`
}

type changesResponse struct {
	Results []struct {
		ID      string          `json:"id"`
		Seq     json.RawMessage `json:"seq"`
		Deleted bool            `json:"deleted"`
		Doc     map[string]any  `json:"doc"`
	} `json:"results"`
	LastSeq json.RawMessage `json:"last_seq"`
}

type bulkDocsRequest struct {
	Docs []map[string]any `json:"docs"`
}

type bulkDocsResponse struct {
	ID     string `json:"id"`
	Rev    string `json:"rev"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func decodeRecord(doc map[string]any) (protocol.Record, error) {
	id, _ := doc["_id"].(string)
	typ, _ := doc["type"].(string)
	if strings.HasPrefix(id, "h:") || typ == "leaf" {
		data, _ := doc["data"].(string)
		encrypted, _ := doc["e_"].(bool)
		return protocol.Record{Chunk: &protocol.Chunk{ID: id, Data: data, Encrypted: encrypted}}, nil
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return protocol.Record{}, err
	}
	var liveDoc protocol.Document
	if err := json.Unmarshal(raw, &liveDoc); err != nil {
		return protocol.Record{}, err
	}
	return protocol.Record{Document: &liveDoc}, nil
}

func skipDocID(id string) bool {
	return strings.HasPrefix(id, "_design/") || strings.HasPrefix(id, "_local/")
}

func rawSeqString(seq json.RawMessage) string {
	if len(seq) == 0 || string(seq) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(seq, &s); err == nil {
		return s
	}
	return string(seq)
}

package couchdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gobsidian-cli/internal/plugins/livesync/protocol"
)

func TestChangesReadsIncrementalDocsAndSince(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vault/_changes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("include_docs") != "true" {
			t.Fatalf("include_docs should be true")
		}
		if r.URL.Query().Get("since") != "12" {
			t.Fatalf("unexpected since: %s", r.URL.Query().Get("since"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"last_seq": "14",
			"results": []any{
				map[string]any{"id": "h:chunk", "seq": "13", "doc": map[string]any{"_id": "h:chunk", "type": "leaf", "data": "hello"}},
				map[string]any{"id": "note.md", "seq": "14", "doc": map[string]any{"_id": "note.md", "_rev": "1-a", "type": "plain", "path": "note.md", "children": []string{"h:chunk"}, "eden": map[string]any{}}},
			},
		})
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	changes, lastSeq, err := client.Changes(t.Context(), "12")
	if err != nil {
		t.Fatalf("Changes returned error: %v", err)
	}
	if lastSeq != "14" {
		t.Fatalf("unexpected last seq: %s", lastSeq)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if changes[0].Record.Chunk == nil || changes[0].Record.Chunk.Data != "hello" {
		t.Fatalf("unexpected chunk change: %#v", changes[0])
	}
	if changes[1].Record.Document == nil || changes[1].Record.Document.Path != "note.md" {
		t.Fatalf("unexpected document change: %#v", changes[1])
	}
}

func TestFetchRecordsReadsAllDocsAndSeparatesChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vault/_all_docs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("include_docs") != "true" {
			t.Fatalf("include_docs should be true")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows": []any{
				map[string]any{"doc": map[string]any{"_id": "h:chunk", "type": "leaf", "data": "hello"}},
				map[string]any{"doc": map[string]any{"_id": "note.md", "_rev": "1-a", "type": "plain", "path": "note.md", "children": []string{"h:chunk"}, "eden": map[string]any{}}},
			},
		})
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	records, err := client.FetchRecords(t.Context())
	if err != nil {
		t.Fatalf("FetchRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}

func TestFetchRecordsByIDPostsKeys(t *testing.T) {
	var request struct {
		Keys []string `json:"keys"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vault/_all_docs" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("include_docs") != "true" {
			t.Fatalf("include_docs should be true")
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows": []any{
				map[string]any{"doc": map[string]any{"_id": "h:chunk", "type": "leaf", "data": "hello"}},
			},
		})
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	records, err := client.FetchRecordsByID(t.Context(), []string{"h:chunk"})
	if err != nil {
		t.Fatalf("FetchRecordsByID returned error: %v", err)
	}
	if len(request.Keys) != 1 || request.Keys[0] != "h:chunk" {
		t.Fatalf("unexpected requested keys: %#v", request.Keys)
	}
	if len(records) != 1 || records[0].Chunk == nil || records[0].Chunk.Data != "hello" {
		t.Fatalf("unexpected records: %#v", records)
	}
}

func TestBulkWriteSkipsExistingChunksAndUsesExistingDocumentRevisions(t *testing.T) {
	var request bulkDocsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vault/h:chunk":
			_ = json.NewEncoder(w).Encode(map[string]any{"_id": "h:chunk", "_rev": "1-chunk"})
		case "/vault/note.md":
			_ = json.NewEncoder(w).Encode(map[string]any{"_id": "note.md", "_rev": "1-old"})
		case "/vault/_bulk_docs":
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode request: %v", err)
			}
			results := make([]bulkDocsResponse, 0, len(request.Docs))
			for _, doc := range request.Docs {
				id, _ := doc["_id"].(string)
				results = append(results, bulkDocsResponse{ID: id, Rev: "2-note", OK: true})
			}
			_ = json.NewEncoder(w).Encode(results)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	responses, err := client.BulkWrite(t.Context(), []protocol.Record{
		{Chunk: &protocol.Chunk{ID: "h:chunk", Data: "hello"}},
		{Document: &protocol.Document{ID: "note.md", Path: "note.md", Type: "plain", Children: []string{"h:chunk"}, Eden: map[string]protocol.EdenChunk{}}},
	})
	if err != nil {
		t.Fatalf("BulkWrite returned error: %v", err)
	}
	if len(request.Docs) != 1 {
		t.Fatalf("expected only document write, got %d docs: %#v", len(request.Docs), request.Docs)
	}
	if request.Docs[0]["_id"] != "note.md" || request.Docs[0]["_rev"] != "1-old" {
		t.Fatalf("document should carry existing rev, got %#v", request.Docs[0])
	}
	if responses["note.md"] != "2-note" {
		t.Fatalf("unexpected responses: %#v", responses)
	}
	if responses["h:chunk"] != "1-chunk" {
		t.Fatalf("existing chunk rev should be preserved: %#v", responses)
	}
}

func TestBulkWriteTreatsChunkConflictAsExistingLeaf(t *testing.T) {
	var chunkGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vault/h:+chunk":
			chunkGets++
			if chunkGets == 1 {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "reason": "missing"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"_id": "h:+chunk", "_rev": "1-existing", "type": "leaf"})
		case "/vault/note.md":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "reason": "missing"})
		case "/vault/_bulk_docs":
			_ = json.NewEncoder(w).Encode([]bulkDocsResponse{
				{ID: "h:+chunk", Error: "conflict", Reason: "Document update conflict."},
				{ID: "note.md", Rev: "2-note", OK: true},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	responses, err := client.BulkWrite(t.Context(), []protocol.Record{
		{Chunk: &protocol.Chunk{ID: "h:+chunk", Data: "hello"}},
		{Document: &protocol.Document{ID: "note.md", Path: "note.md", Type: "plain", Children: []string{"h:+chunk"}, Eden: map[string]protocol.EdenChunk{}}},
	})
	if err != nil {
		t.Fatalf("BulkWrite returned error: %v", err)
	}
	if responses["h:+chunk"] != "1-existing" || responses["note.md"] != "2-note" {
		t.Fatalf("unexpected responses: %#v", responses)
	}
	if chunkGets != 2 {
		t.Fatalf("expected conflict path to re-read chunk, got %d GETs", chunkGets)
	}
}

func TestBulkWriteTreatsChunkConflictWithMissingReadAsExistingChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vault/h:+chunk":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "reason": "missing"})
		case "/vault/note.md":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "reason": "missing"})
		case "/vault/_bulk_docs":
			_ = json.NewEncoder(w).Encode([]bulkDocsResponse{
				{ID: "h:+chunk", Error: "conflict", Reason: "Document update conflict."},
				{ID: "note.md", Rev: "2-note", OK: true},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	responses, err := client.BulkWrite(t.Context(), []protocol.Record{
		{Chunk: &protocol.Chunk{ID: "h:+chunk", Data: "hello"}},
		{Document: &protocol.Document{ID: "note.md", Path: "note.md", Type: "plain", Children: []string{"h:+chunk"}, Eden: map[string]protocol.EdenChunk{}}},
	})
	if err != nil {
		t.Fatalf("BulkWrite returned error: %v", err)
	}
	if responses["note.md"] != "2-note" {
		t.Fatalf("unexpected responses: %#v", responses)
	}
}

func TestBulkWriteRetriesDocumentConflictWithLatestRevision(t *testing.T) {
	var bulkCalls int
	var retryRequest bulkDocsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vault/note.md":
			_ = json.NewEncoder(w).Encode(map[string]any{"_id": "note.md", "_rev": "2-current"})
		case "/vault/_bulk_docs":
			bulkCalls++
			var request bulkDocsRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode request: %v", err)
			}
			if bulkCalls == 1 {
				_ = json.NewEncoder(w).Encode([]bulkDocsResponse{
					{ID: "note.md", Error: "conflict", Reason: "Document update conflict."},
				})
				return
			}
			retryRequest = request
			_ = json.NewEncoder(w).Encode([]bulkDocsResponse{
				{ID: "note.md", Rev: "3-written", OK: true},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	responses, err := client.BulkWrite(t.Context(), []protocol.Record{
		{Document: &protocol.Document{ID: "note.md", Path: "note.md", Type: "plain", Children: []string{}, Eden: map[string]protocol.EdenChunk{}}},
	})
	if err != nil {
		t.Fatalf("BulkWrite returned error: %v", err)
	}
	if bulkCalls != 2 {
		t.Fatalf("expected retry bulk call, got %d", bulkCalls)
	}
	if len(retryRequest.Docs) != 1 || retryRequest.Docs[0]["_rev"] != "2-current" {
		t.Fatalf("retry should use latest rev, got %#v", retryRequest.Docs)
	}
	if responses["note.md"] != "3-written" {
		t.Fatalf("unexpected responses: %#v", responses)
	}
}

func TestBulkWriteTreatsMissingDeletedDocumentAsAlreadyDeleted(t *testing.T) {
	var bulkCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vault/note.md":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "reason": "deleted"})
		case "/vault/_bulk_docs":
			bulkCalls++
			t.Fatal("already deleted tombstone should not be written without a revision")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	if _, err := client.BulkWrite(t.Context(), []protocol.Record{
		{Document: &protocol.Document{ID: "note.md", Path: "note.md", Deleted: true, DeletedP: true}},
	}); err != nil {
		t.Fatalf("BulkWrite returned error: %v", err)
	}
	if bulkCalls != 0 {
		t.Fatalf("unexpected bulk calls: %d", bulkCalls)
	}
}

func TestBulkWriteTreatsMissingDeletedDocumentAfterConflictAsAlreadyDeleted(t *testing.T) {
	var bulkCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vault/note.md":
			if bulkCalls == 0 {
				_ = json.NewEncoder(w).Encode(map[string]any{"_id": "note.md", "_rev": "1-current"})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "reason": "deleted"})
		case "/vault/_bulk_docs":
			bulkCalls++
			if bulkCalls == 1 {
				_ = json.NewEncoder(w).Encode([]bulkDocsResponse{
					{ID: "note.md", Error: "conflict", Reason: "Document update conflict."},
				})
				return
			}
			t.Fatal("already deleted retry tombstone should not be written without a revision")
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	if _, err := client.BulkWrite(t.Context(), []protocol.Record{
		{Document: &protocol.Document{ID: "note.md", Path: "note.md", Deleted: true, DeletedP: true}},
	}); err != nil {
		t.Fatalf("BulkWrite returned error: %v", err)
	}
	if bulkCalls != 1 {
		t.Fatalf("expected only initial bulk call, got %d", bulkCalls)
	}
}

func TestSyncParametersReadsPBKDF2SaltFromLocalDoc(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vault/_local/obsidian_livesync_sync_parameters" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_id":        "_local/obsidian_livesync_sync_parameters",
			"type":       "sync-parameters",
			"pbkdf2salt": "AQIDBA==",
		})
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "vault"})
	salt, err := client.SyncParameters(t.Context())
	if err != nil {
		t.Fatalf("SyncParameters returned error: %v", err)
	}
	if string(salt) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected salt: %#v", salt)
	}
}

func TestGetDocReturnsErrorWhenDatabaseDoesNotExist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "not_found",
			"reason": "Database does not exist.",
		})
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, Database: "missing"})
	_, _, err := client.GetDoc(t.Context(), "_local/obsidian_livesync_sync_parameters")
	if err == nil {
		t.Fatal("expected missing database to return an error")
	}
}

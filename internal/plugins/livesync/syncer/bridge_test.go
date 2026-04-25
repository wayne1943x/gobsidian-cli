package syncer

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gobsidian-cli/internal/plugins/livesync/couchdb"
	"gobsidian-cli/internal/plugins/livesync/protocol"
)

func TestRunBridgeOncePullsRemoteAndSuppressesLoopback(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	store := &memoryCouch{
		records: []protocol.Record{
			{Chunk: &protocol.Chunk{ID: "h:remote", Data: "remote"}},
			{Document: &protocol.Document{ID: "notes/remote.md", Rev: "1-a", Path: "notes/remote.md", Type: "plain", Children: []string{"h:remote"}, Eden: map[string]protocol.EdenChunk{}}},
		},
		lastSeq: "1",
	}

	err := RunBridgeOnce(context.Background(), store, BridgeOptions{
		Root:        root,
		StatePath:   statePath,
		BaseDir:     "notes",
		NowMillis:   2000,
		ForceRemote: true,
	})
	if err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "remote.md"))
	if err != nil || string(got) != "remote" {
		t.Fatalf("remote file not restored, got=%q err=%v", string(got), err)
	}
	if len(store.written) != 0 {
		t.Fatalf("remote pull should not loop back into writes: %#v", store.written)
	}
}

func TestRunBridgeOnceForceRemoteSkipsHiddenRemoteFiles(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	store := &memoryCouch{
		records: []protocol.Record{
			{Chunk: &protocol.Chunk{ID: "h:state", Data: "state"}},
			{Document: &protocol.Document{ID: "hidden-state", Rev: "1-a", Path: ".hidden-state/state.json", Type: "plain", Children: []string{"h:state"}, Eden: map[string]protocol.EdenChunk{}}},
			{Chunk: &protocol.Chunk{ID: "h:note", Data: "note"}},
			{Document: &protocol.Document{ID: "note.md", Rev: "1-b", Path: "note.md", Type: "plain", Children: []string{"h:note"}, Eden: map[string]protocol.EdenChunk{}}},
		},
		lastSeq: "1",
	}

	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 2500, ForceRemote: true}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".hidden-state", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("hidden remote state should not be restored, stat err=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(got) != "note" {
		t.Fatalf("visible note not restored, got=%q err=%v", string(got), err)
	}
}

func TestRunBridgeOncePreservesUntrackedLocalFileOnInitialPull(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("local draft"), 0o644); err != nil {
		t.Fatalf("WriteFile note: %v", err)
	}
	store := &memoryCouch{
		records: []protocol.Record{
			{Chunk: &protocol.Chunk{ID: "h:remote", Data: "remote version"}},
			{Document: &protocol.Document{ID: "note.md", Rev: "1-remote", Path: "note.md", Type: "plain", Children: []string{"h:remote"}, Eden: map[string]protocol.EdenChunk{}}},
		},
		lastSeq: "1",
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 7000, ForceRemote: true}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(got) != "remote version" {
		t.Fatalf("expected remote version in note.md, got=%q err=%v", string(got), err)
	}
	entries, err := filepath.Glob(filepath.Join(root, "note.sync-conflict-*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one conflict file, got %#v", entries)
	}
	conflict, err := os.ReadFile(entries[0])
	if err != nil || string(conflict) != "local draft" {
		t.Fatalf("expected local draft in conflict file, got=%q err=%v", string(conflict), err)
	}
	if len(store.written) != 0 {
		t.Fatalf("force remote should not upload preserved conflict files: %#v", store.written)
	}
}

func TestRunBridgeOncePushesLocalChangesAndDeletes(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "local.md"), []byte("local"), 0o644); err != nil {
		t.Fatalf("WriteFile local: %v", err)
	}
	store := &memoryCouch{lastSeq: "1", revs: map[string]string{}}

	err := RunBridgeOnce(context.Background(), store, BridgeOptions{
		Root:      root,
		StatePath: statePath,
		BaseDir:   "notes",
		NowMillis: 3000,
	})
	if err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if len(store.written) != 2 {
		t.Fatalf("expected chunk and document writes, got %d", len(store.written))
	}
	doc := store.written[1].Document
	if doc == nil || doc.Path != "notes/local.md" || doc.Rev != "" {
		t.Fatalf("unexpected document write: %#v", store.written[1])
	}

	if err := os.Remove(filepath.Join(root, "local.md")); err != nil {
		t.Fatalf("Remove local: %v", err)
	}
	store.written = nil
	err = RunBridgeOnce(context.Background(), store, BridgeOptions{
		Root:      root,
		StatePath: statePath,
		BaseDir:   "notes",
		NowMillis: 4000,
	})
	if err != nil {
		t.Fatalf("RunBridgeOnce delete returned error: %v", err)
	}
	if len(store.written) != 1 || store.written[0].Document == nil || !store.written[0].Document.IsDeleted() {
		t.Fatalf("expected tombstone write, got %#v", store.written)
	}
	if store.written[0].Document.DeletedP {
		t.Fatalf("LiveSync file deletes should keep metadata and use deleted=true, got %#v", store.written[0].Document)
	}
}

func TestRunBridgeOnceForceLocalPushesUnchangedTrackedFiles(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	content := []byte("local wins")
	if err := os.WriteFile(filepath.Join(root, "note.md"), content, 0o644); err != nil {
		t.Fatalf("WriteFile note: %v", err)
	}
	if err := SaveState(statePath, State{
		CouchSince: "4",
		Files: map[string]FileState{
			"note.md": {Hash: hashBytes(content), DocID: "note.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{
			{ID: "note.md", Seq: "5", Record: protocol.Record{Document: &protocol.Document{ID: "note.md", Rev: "2-remote", Path: "note.md", Type: "plain", Children: []string{"h:remote"}, Eden: map[string]protocol.EdenChunk{}}}},
		},
		lastSeq: "5",
	}

	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 8000, ForceLocal: true}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if len(store.written) != 2 {
		t.Fatalf("expected force local to write chunk and document, got %#v", store.written)
	}
	doc := store.written[1].Document
	if doc == nil || doc.ID != "note.md" || doc.Rev != "" {
		t.Fatalf("force local should refresh remote rev in CouchDB layer, got %#v", store.written[1])
	}
}

func TestRunBridgeOnceForceLocalDeletesRemoteOnlyFiles(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "local.md"), []byte("local"), 0o644); err != nil {
		t.Fatalf("WriteFile local: %v", err)
	}
	store := &memoryCouch{
		records: []protocol.Record{
			{Chunk: &protocol.Chunk{ID: "h:remote", Data: "remote"}},
			{Document: &protocol.Document{ID: "remote.md", Rev: "3-remote", Path: "remote.md", Type: "plain", Children: []string{"h:remote"}, Eden: map[string]protocol.EdenChunk{}}},
		},
		lastSeq: "7",
	}

	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 9000, ForceLocal: true}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	var tombstone *protocol.Document
	for _, record := range store.written {
		if record.Document != nil && record.Document.ID == "remote.md" {
			tombstone = record.Document
		}
	}
	if tombstone == nil || !tombstone.IsDeleted() || tombstone.DeletedP || tombstone.Rev != "" {
		t.Fatalf("expected remote-only file tombstone with refreshed rev, got %#v", tombstone)
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	if _, ok := state.Files["remote.md"]; ok {
		t.Fatalf("force-local tombstone should remove remote-only file from state: %#v", state.Files)
	}
}

func TestRunBridgeOnceDoesNotResurrectFileDeletedAfterLocalUpload(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	store := newTrackingCouch()
	if err := os.WriteFile(filepath.Join(root, "quick-delete.md"), []byte("quick"), 0o644); err != nil {
		t.Fatalf("WriteFile quick-delete: %v", err)
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 1000}); err != nil {
		t.Fatalf("initial sync returned error: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "quick-delete.md")); err != nil {
		t.Fatalf("Remove quick-delete: %v", err)
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 2000}); err != nil {
		t.Fatalf("delete sync returned error: %v", err)
	}
	doc := store.documents["quick-delete.md"]
	if doc == nil || !doc.IsDeleted() {
		t.Fatalf("expected tombstone after quick delete, got %#v", doc)
	}
	if _, err := os.Stat(filepath.Join(root, "quick-delete.md")); !os.IsNotExist(err) {
		t.Fatalf("quick-delete.md should remain deleted locally, stat err=%v", err)
	}
}

func TestRunBridgeOnceUsesLiveSyncChunkHash(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "local.md"), []byte("fasd\n"), 0o644); err != nil {
		t.Fatalf("WriteFile local: %v", err)
	}
	store := &memoryCouch{lastSeq: "1"}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 3000}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if len(store.written) < 1 || store.written[0].Chunk == nil {
		t.Fatalf("expected first write to be a chunk, got %#v", store.written)
	}
	if store.written[0].Chunk.ID != "h:2sho5i52uv1xn" {
		t.Fatalf("unexpected chunk id: %s", store.written[0].Chunk.ID)
	}
}

func TestRunBridgeOncePullsEncryptedObfuscatedRemote(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	salt := testSalt()
	store := &memoryCouch{
		records: []protocol.Record{
			{Chunk: &protocol.Chunk{ID: "h:+abc", Data: "%=2ddutJwgMpXQlzFu2rWmkY+TBYd+vxpRI+jH3CPZOHBi2oBrfBfsk/VFXfpbW2L3IusvpHqGYf9LcLWxNulfD2GtyG6QkYIuc55Eog==", Encrypted: true}},
			{Document: &protocol.Document{
				ID:   "f:fixture",
				Rev:  "1-a",
				Path: `/\:%=6U6h8BFVlSp77qa6FAvVQqeJ3LRxfuDtwsphI5SNdYH9xA7lP7m24JCaHRwVGEiCa++aeNAzSzqK0AgbNWcFE6rTJ0utK8mEK14Mw8LMOWWpE226bFmZVrI8oTN0St0CFZuAZBBeGD8TVbk/k90+7Tx2wydd8os/1zTqpkjRpu+YyjnLjcw868uGzaZJ`,
				Type: "plain",
				Eden: map[string]protocol.EdenChunk{},
			}},
		},
		lastSeq: "1",
	}
	err := RunBridgeOnce(context.Background(), store, BridgeOptions{
		Root:                root,
		StatePath:           statePath,
		NowMillis:           3000,
		ForceRemote:         true,
		Passphrase:          "secret-pass",
		PBKDF2Salt:          salt,
		PropertyObfuscation: true,
	})
	if err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "secret", "note.md"))
	if err != nil || string(got) != "hello encrypted\n" {
		t.Fatalf("encrypted remote file not restored, got=%q err=%v", string(got), err)
	}
}

func TestRunBridgeOncePushesEncryptedObfuscatedLocalChange(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.MkdirAll(filepath.Join(root, "secret"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret", "note.md"), []byte("hello encrypted\n"), 0o644); err != nil {
		t.Fatalf("WriteFile note: %v", err)
	}
	store := &memoryCouch{lastSeq: "1"}
	err := RunBridgeOnce(context.Background(), store, BridgeOptions{
		Root:                root,
		StatePath:           statePath,
		NowMillis:           3000,
		Passphrase:          "secret-pass",
		PBKDF2Salt:          testSalt(),
		PropertyObfuscation: true,
	})
	if err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if len(store.written) != 2 {
		t.Fatalf("expected chunk and document writes, got %#v", store.written)
	}
	chunk := store.written[0].Chunk
	if chunk == nil || chunk.ID != "h:+11r30enaj9z36" || !chunk.Encrypted {
		t.Fatalf("unexpected encrypted chunk write: %#v", store.written[0])
	}
	if strings.Contains(chunk.Data, "hello encrypted") || !strings.HasPrefix(chunk.Data, "%=") {
		t.Fatalf("chunk data should be encrypted, got %q", chunk.Data)
	}
	doc := store.written[1].Document
	if doc == nil || !strings.HasPrefix(doc.ID, "f:") || !strings.HasPrefix(doc.Path, `/\:%=`) {
		t.Fatalf("unexpected encrypted document write: %#v", store.written[1])
	}
	if doc.Size != 0 || doc.Mtime != 0 || doc.Ctime != 0 || len(doc.Children) != 0 {
		t.Fatalf("obfuscated document metadata should be hidden: %#v", doc)
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Files["secret/note.md"].RemoteRev != "2-written" {
		t.Fatalf("encrypted obfuscated write should save returned remote rev: %#v", state.Files["secret/note.md"])
	}
}

func TestRunBridgeOncePushesEncryptedObfuscatedLocalDelete(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := SaveState(statePath, State{
		Files: map[string]FileState{
			"secret/note.md": {
				Hash:      hashBytes([]byte("old")),
				DocID:     protocol.PathToID("secret/note.md", "secret-pass", false),
				RemoteRev: "1-old",
			},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{lastSeq: "1"}
	err := RunBridgeOnce(context.Background(), store, BridgeOptions{
		Root:                root,
		StatePath:           statePath,
		NowMillis:           3000,
		Passphrase:          "secret-pass",
		PBKDF2Salt:          testSalt(),
		PropertyObfuscation: true,
	})
	if err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if len(store.written) != 1 || store.written[0].Document == nil {
		t.Fatalf("expected tombstone write, got %#v", store.written)
	}
	doc := store.written[0].Document
	if !doc.IsDeleted() || doc.DeletedP || !strings.HasPrefix(doc.ID, "f:") || !strings.HasPrefix(doc.Path, `/\:%=`) {
		t.Fatalf("expected encrypted obfuscated tombstone, got %#v", doc)
	}
}

func TestRunBridgeOnceAppliesCouchDeletedChanges(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile old: %v", err)
	}
	if err := SaveState(statePath, State{
		CouchSince: "1",
		Files: map[string]FileState{
			"old.md": {Hash: "old", DocID: "notes/old.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{{ID: "notes/old.md", Seq: "2", Deleted: true}},
		lastSeq: "2",
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, BaseDir: "notes", NowMillis: 5000}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("old.md should be removed after remote delete, stat err=%v", err)
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if _, ok := state.Files["old.md"]; ok {
		t.Fatalf("deleted file should be removed from state: %#v", state.Files["old.md"])
	}
}

func TestRunBridgeOncePreservesDirtyLocalFileWhenRemoteDeletes(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "old.md"), []byte("local edit"), 0o644); err != nil {
		t.Fatalf("WriteFile old: %v", err)
	}
	if err := SaveState(statePath, State{
		CouchSince: "1",
		Files: map[string]FileState{
			"old.md": {Hash: hashBytes([]byte("base")), DocID: "old.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{{ID: "old.md", Seq: "2", Deleted: true}},
		lastSeq: "2",
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 8000}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.md")); !os.IsNotExist(err) {
		t.Fatalf("old.md should be removed after remote delete, stat err=%v", err)
	}
	entries, err := filepath.Glob(filepath.Join(root, "old.sync-conflict-*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one conflict file, got %#v", entries)
	}
	conflict, err := os.ReadFile(entries[0])
	if err != nil || string(conflict) != "local edit" {
		t.Fatalf("expected local edit in conflict file, got=%q err=%v", string(conflict), err)
	}
}

func testSalt() []byte {
	return []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
}

func TestRunBridgeOncePreservesLocalConflictWhenRemoteAlsoChanged(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("local edit"), 0o644); err != nil {
		t.Fatalf("WriteFile note: %v", err)
	}
	oldHash := hashBytes([]byte("base"))
	if err := SaveState(statePath, State{
		CouchSince: "1",
		Files: map[string]FileState{
			"note.md": {Hash: oldHash, DocID: "note.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{{ID: "note.md", Seq: "2"}},
		lastSeq: "2",
		records: []protocol.Record{
			{Chunk: &protocol.Chunk{ID: "h:remote", Data: "remote edit"}},
			{Document: &protocol.Document{ID: "note.md", Rev: "2-remote", Path: "note.md", Type: "plain", Children: []string{"h:remote"}, Eden: map[string]protocol.EdenChunk{}}},
		},
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 6000}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(got) != "remote edit" {
		t.Fatalf("expected remote version in note.md, got=%q err=%v", string(got), err)
	}
	entries, err := filepath.Glob(filepath.Join(root, "note.sync-conflict-*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one conflict file, got %#v", entries)
	}
	conflict, err := os.ReadFile(entries[0])
	if err != nil || string(conflict) != "local edit" {
		t.Fatalf("expected local edit in conflict file, got=%q err=%v", string(conflict), err)
	}
}

func TestRunBridgeOnceUsesChangesForIncrementalRemotePull(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := SaveState(statePath, State{
		CouchSince: "1",
		Files: map[string]FileState{
			"note.md": {Hash: hashBytes([]byte("base")), DocID: "note.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{
			{ID: "h:new", Seq: "2", Record: protocol.Record{Chunk: &protocol.Chunk{ID: "h:new", Data: "remote incremental"}}},
			{ID: "note.md", Seq: "3", Record: protocol.Record{Document: &protocol.Document{ID: "note.md", Rev: "2-remote", Path: "note.md", Type: "plain", Children: []string{"h:new"}, Eden: map[string]protocol.EdenChunk{}}}},
		},
		lastSeq: "3",
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 9000}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if store.fetchAllCalls != 0 {
		t.Fatalf("incremental pull should not call FetchRecords, got %d calls", store.fetchAllCalls)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(got) != "remote incremental" {
		t.Fatalf("expected incremental remote content, got=%q err=%v", string(got), err)
	}
}

func TestRunBridgeOnceFetchesMissingChunksForIncrementalDocChange(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := SaveState(statePath, State{
		CouchSince: "1",
		Files: map[string]FileState{
			"note.md": {Hash: hashBytes([]byte("base")), DocID: "note.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{
			{ID: "note.md", Seq: "2", Record: protocol.Record{Document: &protocol.Document{ID: "note.md", Rev: "2-remote", Path: "note.md", Type: "plain", Children: []string{"h:existing"}, Eden: map[string]protocol.EdenChunk{}}}},
		},
		records: []protocol.Record{{Chunk: &protocol.Chunk{ID: "h:existing", Data: "remote via fetched chunk"}}},
		lastSeq: "2",
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 9100}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if store.fetchAllCalls != 0 {
		t.Fatalf("incremental pull should not call FetchRecords, got %d calls", store.fetchAllCalls)
	}
	if store.fetchByIDCalls != 1 {
		t.Fatalf("expected one FetchRecordsByID call, got %d", store.fetchByIDCalls)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(got) != "remote via fetched chunk" {
		t.Fatalf("expected fetched chunk content, got=%q err=%v", string(got), err)
	}
}

func TestRunBridgeOnceFetchesRemoteChunkEvenWhenChangedDocHasEden(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("old local"), 0o644); err != nil {
		t.Fatalf("WriteFile note: %v", err)
	}
	if err := SaveState(statePath, State{
		CouchSince: "1",
		Files: map[string]FileState{
			"note.md": {Hash: hashBytes([]byte("old local")), DocID: "note.md", RemoteRev: "1-old"},
		},
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	store := &memoryCouch{
		changes: []couchdb.Change{
			{ID: "note.md", Seq: "2", Record: protocol.Record{Document: &protocol.Document{
				ID:       "note.md",
				Rev:      "2-remote",
				Path:     "note.md",
				Type:     "plain",
				Children: []string{"h:new"},
				Eden: map[string]protocol.EdenChunk{
					"h:new": {Data: "stale eden data", Epoch: 1},
				},
			}}},
		},
		records: []protocol.Record{{Chunk: &protocol.Chunk{ID: "h:new", Data: "new remote content"}}},
		lastSeq: "2",
	}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 9200}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	if store.fetchByIDCalls != 1 {
		t.Fatalf("expected remote child chunk to be fetched by id, got %d calls", store.fetchByIDCalls)
	}
	got, err := os.ReadFile(filepath.Join(root, "note.md"))
	if err != nil || string(got) != "new remote content" {
		t.Fatalf("expected remote modified content, got=%q err=%v", string(got), err)
	}
}

func TestRunBridgeOnceSavesCouchSince(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	store := &memoryCouch{lastSeq: "42"}
	if err := RunBridgeOnce(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath, NowMillis: 5000}); err != nil {
		t.Fatalf("RunBridgeOnce returned error: %v", err)
	}
	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}
	if state.CouchSince != "42" {
		t.Fatalf("expected saved since, got %q", state.CouchSince)
	}
}

func TestRunBridgeLoopDoesNotRewriteWithoutChanges(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".gobsidian", "state.json")
	if err := os.WriteFile(filepath.Join(root, "stable.md"), []byte("stable"), 0o644); err != nil {
		t.Fatalf("WriteFile stable: %v", err)
	}
	store := &memoryCouch{lastSeq: "1"}
	ticks := make(chan time.Time, 3)
	ticks <- time.UnixMilli(1000)
	ticks <- time.UnixMilli(2000)
	ticks <- time.UnixMilli(3000)
	close(ticks)
	if err := RunBridgeLoop(context.Background(), store, BridgeOptions{Root: root, StatePath: statePath}, ticks); err != nil {
		t.Fatalf("RunBridgeLoop returned error: %v", err)
	}
	writes := 0
	for _, record := range store.written {
		if record.Document != nil && strings.HasSuffix(record.Document.Path, "stable.md") {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("expected stable.md to be written once, got %d writes: %#v", writes, store.written)
	}
}

type memoryCouch struct {
	records        []protocol.Record
	changes        []couchdb.Change
	lastSeq        string
	revs           map[string]string
	written        []protocol.Record
	fetchAllCalls  int
	fetchByIDCalls int
}

type trackingCouch struct {
	seq       int
	lastSeq   string
	changes   []couchdb.Change
	records   []protocol.Record
	documents map[string]*protocol.Document
	chunks    map[string]*protocol.Chunk
}

func newTrackingCouch() *trackingCouch {
	return &trackingCouch{
		documents: map[string]*protocol.Document{},
		chunks:    map[string]*protocol.Chunk{},
		lastSeq:   "0",
	}
}

func (m *trackingCouch) Changes(_ context.Context, since string) ([]couchdb.Change, string, error) {
	if since == m.lastSeq {
		return nil, m.lastSeq, nil
	}
	return m.changes, m.lastSeq, nil
}

func (m *trackingCouch) FetchRecords(context.Context) ([]protocol.Record, error) {
	records := make([]protocol.Record, 0, len(m.chunks)+len(m.documents))
	for _, chunk := range m.chunks {
		records = append(records, protocol.Record{Chunk: chunk})
	}
	for _, doc := range m.documents {
		records = append(records, protocol.Record{Document: doc})
	}
	return records, nil
}

func (m *trackingCouch) FetchRecordsByID(ctx context.Context, ids []string) ([]protocol.Record, error) {
	all, err := m.FetchRecords(ctx)
	if err != nil {
		return nil, err
	}
	return filterRecordsByID(all, ids), nil
}

func (m *trackingCouch) BulkWrite(_ context.Context, records []protocol.Record) (map[string]string, error) {
	out := map[string]string{}
	m.changes = nil
	for _, record := range records {
		m.seq++
		seq := strconv.Itoa(m.seq)
		if record.Chunk != nil {
			chunk := *record.Chunk
			m.chunks[chunk.ID] = &chunk
			out[chunk.ID] = "1-chunk"
			m.changes = append(m.changes, couchdb.Change{ID: chunk.ID, Seq: seq, Record: protocol.Record{Chunk: &chunk}})
			continue
		}
		if record.Document != nil {
			doc := *record.Document
			doc.Rev = strconv.Itoa(m.seq) + "-doc"
			m.documents[doc.ID] = &doc
			out[doc.ID] = doc.Rev
			m.changes = append(m.changes, couchdb.Change{ID: doc.ID, Seq: seq, Deleted: doc.IsDeleted(), Record: protocol.Record{Document: &doc}})
		}
		m.lastSeq = seq
	}
	return out, nil
}

func (m *memoryCouch) Changes(context.Context, string) ([]couchdb.Change, string, error) {
	return m.changes, m.lastSeq, nil
}

func (m *memoryCouch) FetchRecords(context.Context) ([]protocol.Record, error) {
	m.fetchAllCalls++
	return m.records, nil
}

func (m *memoryCouch) FetchRecordsByID(_ context.Context, ids []string) ([]protocol.Record, error) {
	m.fetchByIDCalls++
	return filterRecordsByID(m.records, ids), nil
}

func (m *memoryCouch) BulkWrite(_ context.Context, records []protocol.Record) (map[string]string, error) {
	m.written = append(m.written, records...)
	out := map[string]string{}
	for _, record := range records {
		if record.Chunk != nil {
			out[record.Chunk.ID] = "1-chunk"
			m.records = append(m.records, protocol.Record{Chunk: record.Chunk})
			continue
		}
		if record.Document != nil {
			out[record.Document.ID] = "2-written"
			doc := *record.Document
			doc.Rev = "2-written"
			m.records = append(m.records, protocol.Record{Document: &doc})
		}
	}
	return out, nil
}

func filterRecordsByID(records []protocol.Record, ids []string) []protocol.Record {
	needed := map[string]bool{}
	for _, id := range ids {
		needed[id] = true
	}
	var out []protocol.Record
	for _, record := range records {
		switch {
		case record.Chunk != nil && needed[record.Chunk.ID]:
			out = append(out, record)
		case record.Document != nil && needed[record.Document.ID]:
			out = append(out, record)
		}
	}
	return out
}

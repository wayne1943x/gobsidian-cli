package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gobsidian-cli/internal/plugins/livesync/couchdb"
	"gobsidian-cli/internal/plugins/livesync/protocol"
	"gobsidian-cli/internal/plugins/livesync/vault"
)

type CouchStore interface {
	Changes(context.Context, string) ([]couchdb.Change, string, error)
	FetchRecords(context.Context) ([]protocol.Record, error)
	FetchRecordsByID(context.Context, []string) ([]protocol.Record, error)
	BulkWrite(context.Context, []protocol.Record) (map[string]string, error)
}

type BridgeOptions struct {
	Root                string
	StatePath           string
	BaseDir             string
	DryRun              bool
	NowMillis           int64
	ForceRemote         bool
	ForceLocal          bool
	Passphrase          string
	PBKDF2Salt          []byte
	PropertyObfuscation bool
	CaseSensitive       bool
}

type Status struct {
	StatePath    string
	CouchSince   string
	TrackedFiles int
	LastSync     int64
	LastError    string
}

func RunBridgeOnce(ctx context.Context, store CouchStore, opts BridgeOptions) error {
	if opts.NowMillis == 0 {
		opts.NowMillis = time.Now().UnixMilli()
	}
	lock, err := acquireStateLock(opts.StatePath)
	if err != nil {
		return err
	}
	defer releaseStateLock(lock)
	state, err := LoadState(opts.StatePath)
	if err != nil {
		return err
	}
	next, err := runBridgeOnce(ctx, store, opts, state)
	if err != nil {
		state.LastError = err.Error()
		if !opts.DryRun {
			_ = SaveState(opts.StatePath, state)
		}
		return err
	}
	if opts.DryRun {
		return nil
	}
	next.LastError = ""
	next.LastSync = opts.NowMillis
	return SaveState(opts.StatePath, next)
}

func LoadStatus(statePath string) (Status, error) {
	state, err := LoadState(statePath)
	if err != nil {
		return Status{}, err
	}
	return Status{
		StatePath:    statePath,
		CouchSince:   state.CouchSince,
		TrackedFiles: len(state.Files),
		LastSync:     state.LastSync,
		LastError:    state.LastError,
	}, nil
}

func RunBridgeLoop(ctx context.Context, store CouchStore, opts BridgeOptions, ticks <-chan time.Time) error {
	if err := RunBridgeOnce(ctx, store, opts); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case tick, ok := <-ticks:
			if !ok {
				return nil
			}
			next := opts
			next.NowMillis = tick.UnixMilli()
			if err := RunBridgeOnce(ctx, store, next); err != nil {
				return err
			}
		}
	}
}

func runBridgeOnce(ctx context.Context, store CouchStore, opts BridgeOptions, state State) (State, error) {
	state = state.Clone()
	if state.Files == nil {
		state.Files = map[string]FileState{}
	}
	remoteChanged := opts.ForceRemote || state.CouchSince == ""
	changes, lastSeq, err := store.Changes(ctx, state.CouchSince)
	if err != nil {
		return state, err
	}
	if len(changes) > 0 {
		remoteChanged = true
	}
	if opts.ForceLocal {
		remoteChanged = false
		if err := seedStateFromRemote(ctx, store, opts, &state); err != nil {
			return state, err
		}
	}
	if remoteChanged {
		localBefore, err := vault.Scan(opts.Root)
		if err != nil {
			return state, err
		}
		if opts.ForceRemote || state.CouchSince == "" {
			if err := pullRemote(ctx, store, opts, localBefore, &state); err != nil {
				return state, err
			}
		} else {
			if err := pullRemoteChanges(ctx, store, opts, changes, localBefore, &state); err != nil {
				return state, err
			}
		}
		if err := applyCouchDeletedChanges(changes, opts, localBefore, &state); err != nil {
			return state, err
		}
	}
	if lastSeq != "" {
		state.CouchSince = lastSeq
	}
	if opts.ForceRemote {
		return state, nil
	}
	files, err := vault.Scan(opts.Root)
	if err != nil {
		return state, err
	}
	records, next, err := buildLocalRecords(files, state, opts)
	if err != nil {
		return state, err
	}
	if len(records) > 0 && !opts.DryRun {
		revs, err := store.BulkWrite(ctx, records)
		if err != nil {
			return state, err
		}
		for _, record := range records {
			if record.Document == nil || record.Document.Path == "" {
				continue
			}
			if record.Document.IsDeleted() {
				continue
			}
			localPath, ok := toLocalPath(record.Document.Path, opts.BaseDir)
			if ok {
				if _, tracked := next.Files[localPath]; !tracked {
					ok = false
				}
			}
			if !ok {
				localPath, ok = localPathForDocID(record.Document.ID, opts.BaseDir, next)
				if !ok {
					continue
				}
			}
			fileState := next.Files[localPath]
			fileState.RemoteRev = revs[record.Document.ID]
			fileState.DocID = record.Document.ID
			next.Files[localPath] = fileState
		}
		if _, lastSeq, err := store.Changes(ctx, state.CouchSince); err != nil {
			return state, err
		} else if lastSeq != "" {
			next.CouchSince = lastSeq
		}
	}
	return next, nil
}

func pullRemote(ctx context.Context, store CouchStore, opts BridgeOptions, localBefore map[string]vault.File, state *State) error {
	records, err := store.FetchRecords(ctx)
	if err != nil {
		return err
	}
	codec := protocol.NewCodec(protocol.CodecOptions{
		Passphrase:                  opts.Passphrase,
		PBKDF2Salt:                  opts.PBKDF2Salt,
		PropertyObfuscation:         opts.PropertyObfuscation,
		HandleFilenameCaseSensitive: opts.CaseSensitive,
	})
	records, err = codec.DecodeRecords(records)
	if err != nil {
		return err
	}
	return applyRemoteRecords(records, opts, localBefore, state)
}

func seedStateFromRemote(ctx context.Context, store CouchStore, opts BridgeOptions, state *State) error {
	records, err := store.FetchRecords(ctx)
	if err != nil {
		return err
	}
	codec := protocol.NewCodec(protocol.CodecOptions{
		Passphrase:                  opts.Passphrase,
		PBKDF2Salt:                  opts.PBKDF2Salt,
		PropertyObfuscation:         opts.PropertyObfuscation,
		HandleFilenameCaseSensitive: opts.CaseSensitive,
	})
	records, err = codec.DecodeRecords(records)
	if err != nil {
		return err
	}
	projector := protocol.NewProjector()
	if err := projector.Apply(records); err != nil {
		return err
	}
	remoteDocs := map[string]protocol.Document{}
	for _, record := range records {
		if record.Document == nil || record.Document.Path == "" || record.Document.IsDeleted() {
			continue
		}
		localPath, ok := toLocalPath(record.Document.Path, opts.BaseDir)
		if !ok || isHiddenLocalPath(localPath) {
			continue
		}
		remoteDocs[record.Document.Path] = *record.Document
	}
	remoteFiles, err := projector.Files()
	if err != nil {
		return err
	}
	for remotePath, file := range remoteFiles {
		localPath, ok := toLocalPath(remotePath, opts.BaseDir)
		if !ok || isHiddenLocalPath(localPath) {
			continue
		}
		doc := remoteDocs[remotePath]
		state.Files[localPath] = FileState{
			Hash:      hashBytes(file.Content),
			DocID:     doc.ID,
			RemoteRev: doc.Rev,
			Mtime:     file.Mtime,
			Size:      int64(len(file.Content)),
		}
	}
	return nil
}

func pullRemoteChanges(ctx context.Context, store CouchStore, opts BridgeOptions, changes []couchdb.Change, localBefore map[string]vault.File, state *State) error {
	records := make([]protocol.Record, 0, len(changes))
	missingChangeIDs := []string{}
	for _, change := range changes {
		if change.Deleted {
			continue
		}
		if change.Record.Chunk == nil && change.Record.Document == nil {
			missingChangeIDs = append(missingChangeIDs, change.ID)
			continue
		}
		records = append(records, change.Record)
	}
	if len(missingChangeIDs) > 0 {
		fetched, err := store.FetchRecordsByID(ctx, missingChangeIDs)
		if err != nil {
			return err
		}
		records = append(records, fetched...)
	}
	if len(records) == 0 {
		return nil
	}
	codec := protocol.NewCodec(protocol.CodecOptions{
		Passphrase:                  opts.Passphrase,
		PBKDF2Salt:                  opts.PBKDF2Salt,
		PropertyObfuscation:         opts.PropertyObfuscation,
		HandleFilenameCaseSensitive: opts.CaseSensitive,
	})
	records, err := codec.DecodeRecords(records)
	if err != nil {
		return err
	}
	missingChunkIDs := missingChunksForChangedDocs(records)
	if len(missingChunkIDs) > 0 {
		fetched, err := store.FetchRecordsByID(ctx, missingChunkIDs)
		if err != nil {
			return err
		}
		fetched, err = codec.DecodeRecords(fetched)
		if err != nil {
			return err
		}
		records = append(records, fetched...)
	}
	return applyRemoteRecords(records, opts, localBefore, state)
}

func missingChunksForChangedDocs(records []protocol.Record) []string {
	available := map[string]bool{}
	for _, record := range records {
		if record.Chunk != nil {
			available[record.Chunk.ID] = true
		}
	}
	needed := map[string]bool{}
	for _, record := range records {
		if record.Document == nil || record.Document.IsDeleted() {
			continue
		}
		for _, child := range record.Document.Children {
			if available[child] {
				continue
			}
			needed[child] = true
		}
	}
	out := make([]string, 0, len(needed))
	for id := range needed {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func applyRemoteRecords(records []protocol.Record, opts BridgeOptions, localBefore map[string]vault.File, state *State) error {
	projector := protocol.NewProjector()
	if err := projector.Apply(records); err != nil {
		return err
	}
	remoteDocs := map[string]protocol.Document{}
	for _, record := range records {
		if record.Document == nil || record.Document.Path == "" {
			continue
		}
		localPath, ok := toLocalPath(record.Document.Path, opts.BaseDir)
		if !ok || isHiddenLocalPath(localPath) {
			continue
		}
		remoteDocs[record.Document.Path] = *record.Document
	}
	remoteFiles, err := projector.Files()
	if err != nil {
		return err
	}
	snapshot := map[string]vault.File{}
	for remotePath, file := range remoteFiles {
		localPath, ok := toLocalPath(remotePath, opts.BaseDir)
		if !ok || isHiddenLocalPath(localPath) {
			continue
		}
		doc := remoteDocs[remotePath]
		if conflict, ok := conflictFile(localPath, file, localBefore, *state, doc, opts.NowMillis); ok {
			snapshot[conflict.Path] = conflict
		}
		snapshot[localPath] = vault.File{Path: localPath, Content: file.Content, Mtime: file.Mtime}
		state.Files[localPath] = FileState{
			Hash:      hashBytes(file.Content),
			DocID:     doc.ID,
			RemoteRev: doc.Rev,
			Mtime:     file.Mtime,
			Size:      int64(len(file.Content)),
		}
	}
	for remotePath, file := range projector.DeletedFiles() {
		localPath, ok := toLocalPath(remotePath, opts.BaseDir)
		if !ok || isHiddenLocalPath(localPath) {
			continue
		}
		snapshot[localPath] = vault.File{Path: localPath, Deleted: true, Mtime: file.Mtime}
		delete(state.Files, localPath)
	}
	if opts.DryRun {
		return nil
	}
	return vault.WriteSnapshot(opts.Root, snapshot)
}

func applyCouchDeletedChanges(changes []couchdb.Change, opts BridgeOptions, localBefore map[string]vault.File, state *State) error {
	snapshot := map[string]vault.File{}
	for _, change := range changes {
		if !change.Deleted {
			continue
		}
		localPath, ok := localPathForDocID(change.ID, opts.BaseDir, *state)
		if !ok || isHiddenLocalPath(localPath) {
			continue
		}
		if conflict, ok := deletedConflictFile(localPath, localBefore, *state, opts.NowMillis); ok {
			snapshot[conflict.Path] = conflict
		}
		snapshot[localPath] = vault.File{Path: localPath, Deleted: true}
		delete(state.Files, localPath)
	}
	if len(snapshot) == 0 || opts.DryRun {
		return nil
	}
	return vault.WriteSnapshot(opts.Root, snapshot)
}

func buildLocalRecords(files map[string]vault.File, state State, opts BridgeOptions) ([]protocol.Record, State, error) {
	next := state.Clone()
	if next.Files == nil {
		next.Files = map[string]FileState{}
	}
	paths := make([]string, 0, len(files))
	for localPath := range files {
		paths = append(paths, localPath)
	}
	sort.Strings(paths)

	var records []protocol.Record
	codec := protocol.NewCodec(protocol.CodecOptions{
		Passphrase:                  opts.Passphrase,
		PBKDF2Salt:                  opts.PBKDF2Salt,
		PropertyObfuscation:         opts.PropertyObfuscation,
		HandleFilenameCaseSensitive: opts.CaseSensitive,
	})
	for _, localPath := range paths {
		file := files[localPath]
		hash := file.Hash
		if hash == "" {
			hash = hashBytes(file.Content)
		}
		if previous, ok := state.Files[localPath]; ok && previous.Hash == hash && !opts.ForceLocal {
			continue
		}
		remotePath := toRemotePath(localPath, opts.BaseDir)
		chunkIDs := []string{}
		for _, piece := range protocol.SplitTextRabinKarp(string(file.Content)) {
			chunk, err := codec.EncodeChunk(piece)
			if err != nil {
				return nil, state, err
			}
			chunkIDs = append(chunkIDs, chunk.ID)
			records = append(records, protocol.Record{Chunk: &chunk})
		}
		previous := state.Files[localPath]
		docID := previous.DocID
		if docID == "" {
			docID = pathToID(remotePath, opts.Passphrase, opts.PropertyObfuscation, opts.CaseSensitive)
		}
		doc := &protocol.Document{
			ID:       docID,
			Rev:      chooseDocumentRev(previous.RemoteRev, opts.ForceLocal),
			Path:     remotePath,
			Ctime:    chooseMtime(file.Mtime, opts.NowMillis),
			Mtime:    chooseMtime(file.Mtime, opts.NowMillis),
			Size:     int64(len(file.Content)),
			Type:     "plain",
			Children: chunkIDs,
			Eden:     map[string]protocol.EdenChunk{},
		}
		encodedDoc, err := codec.EncodeDocument(*doc)
		if err != nil {
			return nil, state, err
		}
		doc = &encodedDoc
		docID = doc.ID
		records = append(records, protocol.Record{Document: doc})
		next.Files[localPath] = FileState{
			Hash:      hash,
			DocID:     docID,
			RemoteRev: chooseDocumentRev(previous.RemoteRev, opts.ForceLocal),
			Mtime:     file.Mtime,
			Size:      int64(len(file.Content)),
		}
	}

	statePaths := make([]string, 0, len(state.Files))
	for localPath := range state.Files {
		statePaths = append(statePaths, localPath)
	}
	sort.Strings(statePaths)
	for _, localPath := range statePaths {
		if _, ok := files[localPath]; ok {
			continue
		}
		previous := state.Files[localPath]
		if previous.DocID == "" {
			continue
		}
		remotePath := toRemotePath(localPath, opts.BaseDir)
		doc := protocol.Document{
			ID:      previous.DocID,
			Rev:     chooseDocumentRev(previous.RemoteRev, opts.ForceLocal),
			Path:    remotePath,
			Mtime:   opts.NowMillis,
			Type:    "plain",
			Deleted: true,
			Eden:    map[string]protocol.EdenChunk{},
		}
		encodedDoc, err := codec.EncodeDocument(doc)
		if err != nil {
			return nil, state, err
		}
		records = append(records, protocol.Record{Document: &encodedDoc})
		delete(next.Files, localPath)
	}
	return records, next, nil
}

func chooseMtime(fileMtime, fallback int64) int64 {
	if fileMtime > 0 {
		return fileMtime
	}
	return fallback
}

func chooseDocumentRev(remoteRev string, forceLocal bool) string {
	if forceLocal {
		return ""
	}
	return remoteRev
}

func toLocalPath(remotePath, baseDir string) (string, bool) {
	remotePath = cleanSlash(remotePath)
	baseDir = cleanSlash(baseDir)
	if baseDir == "" {
		return remotePath, remotePath != ""
	}
	if remotePath == baseDir {
		return "", false
	}
	prefix := baseDir + "/"
	if !strings.HasPrefix(remotePath, prefix) {
		return "", false
	}
	local := strings.TrimPrefix(remotePath, prefix)
	return local, local != ""
}

func isHiddenLocalPath(localPath string) bool {
	for _, part := range strings.Split(cleanSlash(localPath), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func toRemotePath(localPath, baseDir string) string {
	localPath = cleanSlash(localPath)
	baseDir = cleanSlash(baseDir)
	if baseDir == "" {
		return localPath
	}
	return path.Join(baseDir, localPath)
}

func cleanSlash(value string) string {
	value = strings.TrimSpace(filepathSlash(value))
	value = strings.Trim(value, "/")
	if value == "." {
		return ""
	}
	return value
}

func filepathSlash(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func conflictFile(localPath string, remoteFile protocol.File, localBefore map[string]vault.File, state State, remoteDoc protocol.Document, nowMillis int64) (vault.File, bool) {
	previous, tracked := state.Files[localPath]
	localFile, exists := localBefore[localPath]
	if !exists {
		return vault.File{}, false
	}
	if localFile.Hash == "" {
		localFile.Hash = hashBytes(localFile.Content)
	}
	if !tracked {
		if localFile.Hash == hashBytes(remoteFile.Content) {
			return vault.File{}, false
		}
		conflictPath := makeConflictPath(localPath, nowMillis)
		return vault.File{Path: conflictPath, Content: localFile.Content, Mtime: localFile.Mtime}, true
	}
	if previous.RemoteRev == "" || remoteDoc.Rev == "" || previous.RemoteRev == remoteDoc.Rev {
		return vault.File{}, false
	}
	if localFile.Hash == previous.Hash || localFile.Hash == hashBytes(remoteFile.Content) {
		return vault.File{}, false
	}
	conflictPath := makeConflictPath(localPath, nowMillis)
	return vault.File{Path: conflictPath, Content: localFile.Content, Mtime: localFile.Mtime}, true
}

func deletedConflictFile(localPath string, localBefore map[string]vault.File, state State, nowMillis int64) (vault.File, bool) {
	previous, tracked := state.Files[localPath]
	localFile, exists := localBefore[localPath]
	if !tracked || !exists {
		return vault.File{}, false
	}
	if localFile.Hash == "" {
		localFile.Hash = hashBytes(localFile.Content)
	}
	if localFile.Hash == previous.Hash {
		return vault.File{}, false
	}
	conflictPath := makeConflictPath(localPath, nowMillis)
	return vault.File{Path: conflictPath, Content: localFile.Content, Mtime: localFile.Mtime}, true
}

func makeConflictPath(localPath string, nowMillis int64) string {
	ext := filepath.Ext(localPath)
	base := strings.TrimSuffix(localPath, ext)
	return fmt.Sprintf("%s.sync-conflict-%d%s", base, nowMillis, ext)
}

func pathToID(path, passphrase string, enabled, caseSensitive bool) string {
	if enabled && passphrase != "" {
		return protocol.PathToID(path, passphrase, caseSensitive)
	}
	if strings.HasPrefix(path, "_") {
		return "/" + path
	}
	return path
}

func localPathForDocID(docID, baseDir string, state State) (string, bool) {
	for localPath, fileState := range state.Files {
		if fileState.DocID == docID {
			return localPath, true
		}
	}
	remotePath := docID
	if strings.HasPrefix(remotePath, "/_") {
		remotePath = strings.TrimPrefix(remotePath, "/")
	}
	return toLocalPath(remotePath, baseDir)
}

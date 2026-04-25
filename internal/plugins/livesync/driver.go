package livesync

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugin"
	"gobsidian-cli/internal/plugins/livesync/couchdb"
	"gobsidian-cli/internal/plugins/livesync/syncer"
	"gobsidian-cli/internal/plugins/livesync/vault"
)

const PluginName = "livesync"

type Driver struct {
	logger *zap.Logger
}

func New(logger *zap.Logger) *Driver {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Driver{logger: logger}
}

func Register(registry interface {
	Register(string, plugin.Driver) error
}, logger *zap.Logger) error {
	return registry.Register(PluginName, New(logger))
}

func (d *Driver) Sync(ctx context.Context, target config.Target, syncOpts plugin.SyncOptions) (plugin.SyncResult, error) {
	start := time.Now()
	couch := target.LiveSync.CouchDB
	store := couchdb.New(couchdb.Config{
		URL:      couch.URL,
		Database: couch.DB,
		Username: couch.Username,
		Password: couch.Password,
	})
	opts := syncer.BridgeOptions{
		Root:                target.Vault.Path,
		StatePath:           statePath(target),
		BaseDir:             couch.BaseDir,
		DryRun:              couch.DryRun,
		ForceRemote:         syncOpts.ForceRemote,
		ForceLocal:          syncOpts.ForceLocal,
		Passphrase:          couch.Passphrase,
		PropertyObfuscation: couch.PropertyObfuscation,
	}
	if opts.Passphrase != "" {
		salt, err := store.SyncParameters(ctx)
		if err != nil {
			return plugin.SyncResult{Vault: target.Name, Plugin: PluginName, Duration: time.Since(start)}, fmt.Errorf("%s: %w", target.Name, err)
		}
		opts.PBKDF2Salt = salt
	}
	if err := syncer.RunBridgeOnce(ctx, store, opts); err != nil {
		return plugin.SyncResult{Vault: target.Name, Plugin: PluginName, Duration: time.Since(start)}, err
	}
	status, err := syncer.LoadStatus(opts.StatePath)
	if err != nil {
		return plugin.SyncResult{Vault: target.Name, Plugin: PluginName, Duration: time.Since(start)}, err
	}
	return plugin.SyncResult{
		Vault:        target.Name,
		Plugin:       PluginName,
		FilesRead:    status.TrackedFiles,
		FilesWritten: status.TrackedFiles,
		CouchSince:   status.CouchSince,
		Duration:     time.Since(start),
	}, nil
}

func (d *Driver) Status(_ context.Context, target config.Target) (plugin.StatusResult, error) {
	path := statePath(target)
	status, err := syncer.LoadStatus(path)
	if err != nil {
		return plugin.StatusResult{
			Vault:     target.Name,
			Plugin:    PluginName,
			VaultPath: target.Vault.Path,
			StatePath: path,
		}, err
	}
	return plugin.StatusResult{
		Vault:        target.Name,
		Plugin:       PluginName,
		VaultPath:    target.Vault.Path,
		StatePath:    path,
		CouchSince:   status.CouchSince,
		TrackedFiles: status.TrackedFiles,
		LastSync:     status.LastSync,
		LastError:    status.LastError,
	}, nil
}

func statePath(target config.Target) string {
	if target.State.Path != "" {
		return target.State.Path
	}
	return filepath.Join(target.Vault.Path, vault.StateDir, "state.json")
}

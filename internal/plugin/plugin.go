package plugin

import (
	"context"
	"fmt"
	"time"

	"gobsidian-cli/internal/config"
)

type Driver interface {
	Sync(context.Context, config.Target, SyncOptions) (SyncResult, error)
	Status(context.Context, config.Target) (StatusResult, error)
}

type SyncOptions struct {
	ForceRemote bool
	ForceLocal  bool
}

type Registry struct {
	drivers map[string]Driver
}

type SyncResult struct {
	Vault        string        `json:"vault"`
	Plugin       string        `json:"plugin"`
	FilesRead    int           `json:"files_read"`
	FilesWritten int           `json:"files_written"`
	RemoteWrites int           `json:"remote_writes"`
	Conflicts    int           `json:"conflicts"`
	CouchSince   string        `json:"couch_since,omitempty"`
	Duration     time.Duration `json:"duration"`
	Error        string        `json:"error,omitempty"`
}

type StatusResult struct {
	Vault        string `json:"vault"`
	Plugin       string `json:"plugin"`
	VaultPath    string `json:"vault_path"`
	StatePath    string `json:"state_path"`
	CouchSince   string `json:"couch_since,omitempty"`
	TrackedFiles int    `json:"tracked_files"`
	LastSync     int64  `json:"last_sync,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	Error        string `json:"error,omitempty"`
}

func NewRegistry() *Registry {
	return &Registry{drivers: map[string]Driver{}}
}

func (r *Registry) Register(name string, driver Driver) error {
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if driver == nil {
		return fmt.Errorf("plugin %q driver is nil", name)
	}
	if _, ok := r.drivers[name]; ok {
		return fmt.Errorf("plugin %q already registered", name)
	}
	r.drivers[name] = driver
	return nil
}

func (r *Registry) Get(name string) (Driver, error) {
	driver, ok := r.drivers[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not registered", name)
	}
	return driver, nil
}

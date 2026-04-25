package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugin"
)

func TestRunSyncOutputsJSONAndFiltersVault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`version: 1
targets:
  - name: personal
    plugin: noop
    vault: {path: /vault/personal}
  - name: work
    plugin: noop
    vault: {path: /vault/work}
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reg := plugin.NewRegistry()
	_ = reg.Register("noop", noopDriver{})
	var stdout, stderr bytes.Buffer
	err := Run([]string{"sync", "--config", cfgPath, "--vault", "work"}, &stdout, &stderr, reg)
	if err != nil {
		t.Fatalf("Run returned error: %v stderr=%s", err, stderr.String())
	}
	var out struct {
		OK     bool `json:"ok"`
		Vaults []struct {
			Vault string `json:"vault"`
		} `json:"vaults"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout is not json: %v\n%s", err, stdout.String())
	}
	if !out.OK || len(out.Vaults) != 1 || out.Vaults[0].Vault != "work" {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRunSyncRejectsConflictingForceFlags(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`version: 1
targets:
  - name: personal
    plugin: noop
    vault: {path: /vault/personal}
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reg := plugin.NewRegistry()
	_ = reg.Register("noop", noopDriver{})
	var stdout, stderr bytes.Buffer
	err := Run([]string{"sync", "--config", cfgPath, "--force-remote", "--force-local"}, &stdout, &stderr, reg)
	if err == nil {
		t.Fatalf("expected conflicting force flags to fail, stdout=%s", stdout.String())
	}
}

func TestRunReadRequiresVaultWhenMultipleConfigured(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`version: 1
targets:
  - name: first
    plugin: noop
    vault: {path: /vault/first}
  - name: second
    plugin: noop
    vault: {path: /vault/second}
`), 0o600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	var stdout, stderr bytes.Buffer
	err := Run([]string{"read", "note.md", "--config", cfgPath}, &stdout, &stderr, plugin.NewRegistry())
	if err == nil {
		t.Fatalf("expected multiple vault error, got err=%v stdout=%s", err, stdout.String())
	}
}

func TestRunStatusReturnsErrorForMissingVault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`version: 1
targets:
  - name: personal
    plugin: noop
    vault: {path: /vault/personal}
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reg := plugin.NewRegistry()
	_ = reg.Register("noop", noopDriver{})
	var stdout, stderr bytes.Buffer
	err := Run([]string{"status", "--config", cfgPath, "--vault", "missing"}, &stdout, &stderr, reg)
	if err == nil {
		t.Fatalf("expected missing vault to fail, stdout=%s", stdout.String())
	}
}

type noopDriver struct{}

func (noopDriver) Sync(_ context.Context, target config.Target, _ plugin.SyncOptions) (plugin.SyncResult, error) {
	return plugin.SyncResult{Vault: target.Name, FilesWritten: 1}, nil
}

func (noopDriver) Status(_ context.Context, target config.Target) (plugin.StatusResult, error) {
	return plugin.StatusResult{Vault: target.Name, VaultPath: target.Vault.Path}, nil
}

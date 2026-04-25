package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesTargetsWithPerTargetPluginAndExpandsEnv(t *testing.T) {
	t.Setenv("COUCHDB_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    plugin: livesync
    vault:
      path: /vault/personal
    state:
      path: /state/personal.json
    livesync:
      couchdb:
        url: http://couchdb:5984
        db: obsidian_personal
        username: root
        password: ${COUCHDB_PASSWORD}
        passphrase: ""
        property_obfuscation: true
        base_dir: notes
        dry_run: false
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Version != 1 || len(cfg.Targets) != 1 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	target := cfg.Targets[0]
	if target.Name != "personal" || target.Vault.Path != "/vault/personal" {
		t.Fatalf("unexpected target: %#v", target)
	}
	if target.Plugin != "livesync" {
		t.Fatalf("unexpected target plugin: %q", target.Plugin)
	}
	if target.LiveSync.CouchDB.Password != "secret" || target.LiveSync.CouchDB.BaseDir != "notes" {
		t.Fatalf("env/default config not decoded: %#v", target.LiveSync.CouchDB)
	}
}

func TestLoadRejectsDuplicateTargetsAndUnknownPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    plugin: livesync
    vault: {path: /vault/personal}
    livesync:
      couchdb: {url: http://localhost:5984, db: obsidian_personal}
  - name: personal
    plugin: livesync
    vault: {path: /vault/personal2}
    livesync:
      couchdb: {url: http://localhost:5984, db: obsidian_personal2}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected duplicate target to fail")
	}
}

func TestLoadRejectsTopLevelPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
plugin: livesync
targets:
  - name: personal
    plugin: livesync
    vault: {path: /vault/personal}
    livesync:
      couchdb: {url: http://localhost:5984, db: obsidian_personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected top-level plugin field to fail")
	}
}

func TestLoadRejectsMissingTargetPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    vault: {path: /vault/personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected missing target plugin to fail")
	}
}

func TestLoadRejectsLegacyLiveSyncCouchDBTargetKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    plugin: livesync
    vault: {path: /vault/personal}
    livesync_couchdb: {url: http://localhost:5984, db: obsidian_personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected legacy livesync_couchdb target key to fail")
	}
}

func TestLoadRejectsGenericDBTargetKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    plugin: livesync
    vault: {path: /vault/personal}
    db: {url: http://localhost:5984, db: obsidian_personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected generic db target key to fail")
	}
}

func TestLoadRejectsDirectCouchDBTargetKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    plugin: livesync
    vault: {path: /vault/personal}
    couchdb: {url: http://localhost:5984, db: obsidian_personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected direct couchdb target key to fail")
	}
}

func TestLoadRejectsLegacyTargetCouchDBPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
targets:
  - name: personal
    plugin: livesync-couchdb
    vault: {path: /vault/personal}
    livesync:
      couchdb: {url: http://localhost:5984, db: obsidian_personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected legacy livesync-couchdb plugin name to fail")
	}
}

func TestLoadRejectsLegacyPluginSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte(`version: 1
plugin_settings:
  targets:
    - name: personal
      plugin: livesync
      vault: {path: /vault/personal}
      livesync:
        couchdb: {url: http://localhost:5984, db: obsidian_personal}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected legacy plugin_settings to fail")
	}
}

func TestFilterTargetsDefaultsToAllAndRejectsMissing(t *testing.T) {
	cfg := Config{Targets: []Target{{Name: "personal"}, {Name: "work"}}}
	all, err := cfg.FilterTargets("")
	if err != nil {
		t.Fatalf("FilterTargets default returned error: %v", err)
	}
	if len(all.Targets) != 2 {
		t.Fatalf("expected all targets, got %#v", all.Targets)
	}
	one, err := cfg.FilterTargets("work")
	if err != nil {
		t.Fatalf("FilterTargets work returned error: %v", err)
	}
	if len(one.Targets) != 1 || one.Targets[0].Name != "work" {
		t.Fatalf("expected work only, got %#v", one.Targets)
	}
	if _, err := cfg.FilterTargets("all"); err == nil {
		t.Fatal("literal all should not be special")
	}
}

func TestResolvePathSearchOrder(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	etc := filepath.Join(tmp, "etc")
	cwd := filepath.Join(tmp, "cwd")
	for _, dir := range []string{filepath.Join(home, ".gobsidian"), filepath.Join(etc, "gobsidian"), cwd} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	homeCfg := filepath.Join(home, ".gobsidian", "config.yaml")
	etcCfg := filepath.Join(etc, "gobsidian", "config.yaml")
	cwdCfg := filepath.Join(cwd, "config.yaml")
	for _, path := range []string{homeCfg, etcCfg, cwdCfg} {
		if err := os.WriteFile(path, []byte("version: 1\ntargets: []\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	t.Setenv("HOME", home)
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldwd) }()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	got, err := ResolvePathWithRoots("", etc)
	if err != nil || got != homeCfg {
		t.Fatalf("expected home config, got %q err=%v", got, err)
	}
	_ = os.Remove(homeCfg)
	got, err = ResolvePathWithRoots("", etc)
	if err != nil || got != etcCfg {
		t.Fatalf("expected etc config, got %q err=%v", got, err)
	}
	_ = os.Remove(etcCfg)
	got, err = ResolvePathWithRoots("", etc)
	if err != nil {
		t.Fatalf("ResolvePathWithRoots returned error: %v", err)
	}
	gotEval, _ := filepath.EvalSymlinks(got)
	wantEval, _ := filepath.EvalSymlinks(cwdCfg)
	if gotEval != wantEval {
		t.Fatalf("expected cwd config, got %q", got)
	}
	if got, err := ResolvePathWithRoots("custom.yaml", etc); err != nil || got != "custom.yaml" {
		t.Fatalf("explicit config should win, got %q err=%v", got, err)
	}
}

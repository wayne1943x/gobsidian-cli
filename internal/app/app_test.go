package app

import (
	"context"
	"errors"
	"testing"

	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugin"
)

type appFakeDriver struct {
	err  error
	opts []plugin.SyncOptions
	seen []string
}

func (d *appFakeDriver) Sync(_ context.Context, target config.Target, opts plugin.SyncOptions) (plugin.SyncResult, error) {
	d.opts = append(d.opts, opts)
	d.seen = append(d.seen, target.Name)
	return plugin.SyncResult{Vault: target.Name, FilesWritten: 1}, d.err
}

func (d *appFakeDriver) Status(_ context.Context, target config.Target) (plugin.StatusResult, error) {
	return plugin.StatusResult{Vault: target.Name, TrackedFiles: 1}, d.err
}

func TestSyncRunsSelectedTargetsAndReportsErrors(t *testing.T) {
	reg := plugin.NewRegistry()
	okDriver := &appFakeDriver{}
	badDriver := &appFakeDriver{err: errors.New("boom")}
	_ = reg.Register("ok", okDriver)
	_ = reg.Register("bad", badDriver)
	runner := New(reg)
	cfg := config.Config{Targets: []config.Target{{Name: "personal", Plugin: "bad"}, {Name: "work", Plugin: "bad"}}}
	res := runner.Sync(context.Background(), cfg, plugin.SyncOptions{})
	if res.OK || len(res.Vaults) != 2 || len(res.Errors) != 2 {
		t.Fatalf("unexpected sync response: %#v", res)
	}
	one, err := cfg.FilterTargets("personal")
	if err != nil {
		t.Fatalf("FilterTargets: %v", err)
	}
	one.Targets[0].Plugin = "ok"
	res = runner.Sync(context.Background(), one, plugin.SyncOptions{ForceRemote: true})
	if !res.OK || len(res.Vaults) != 1 || res.Vaults[0].Vault != "personal" {
		t.Fatalf("unexpected selected sync response: %#v", res)
	}
	if len(okDriver.opts) != 1 || !okDriver.opts[0].ForceRemote {
		t.Fatalf("sync options were not forwarded: %#v", okDriver.opts)
	}
}

func TestSyncDispatchesEachTargetToItsPlugin(t *testing.T) {
	reg := plugin.NewRegistry()
	firstDriver := &appFakeDriver{}
	secondDriver := &appFakeDriver{}
	_ = reg.Register("first-plugin", firstDriver)
	_ = reg.Register("second-plugin", secondDriver)
	runner := New(reg)
	cfg := config.Config{Targets: []config.Target{
		{Name: "personal", Plugin: "first-plugin"},
		{Name: "work", Plugin: "second-plugin"},
	}}

	res := runner.Sync(context.Background(), cfg, plugin.SyncOptions{})

	if !res.OK || len(res.Vaults) != 2 {
		t.Fatalf("unexpected sync response: %#v", res)
	}
	if len(firstDriver.seen) != 1 || firstDriver.seen[0] != "personal" {
		t.Fatalf("first driver saw %#v", firstDriver.seen)
	}
	if len(secondDriver.seen) != 1 || secondDriver.seen[0] != "work" {
		t.Fatalf("second driver saw %#v", secondDriver.seen)
	}
	if res.Vaults[0].Plugin != "first-plugin" || res.Vaults[1].Plugin != "second-plugin" {
		t.Fatalf("plugins not reported per target: %#v", res.Vaults)
	}
}

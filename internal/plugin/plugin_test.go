package plugin

import (
	"context"
	"testing"

	"gobsidian-cli/internal/config"
)

type fakeDriver struct{}

func (fakeDriver) Sync(context.Context, config.Target, SyncOptions) (SyncResult, error) {
	return SyncResult{Vault: "personal", Plugin: "fake", FilesWritten: 1}, nil
}

func (fakeDriver) Status(context.Context, config.Target) (StatusResult, error) {
	return StatusResult{Vault: "personal", Plugin: "fake", TrackedFiles: 2}, nil
}

func TestRegistryRegistersAndFindsDrivers(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("fake", fakeDriver{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	driver, err := reg.Get("fake")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	res, err := driver.Sync(context.Background(), config.Target{Name: "personal"}, SyncOptions{})
	if err != nil || res.FilesWritten != 1 {
		t.Fatalf("unexpected sync result %#v err=%v", res, err)
	}
	if _, err := reg.Get("missing"); err == nil {
		t.Fatal("expected missing driver to fail")
	}
	if err := reg.Register("fake", fakeDriver{}); err == nil {
		t.Fatal("expected duplicate driver registration to fail")
	}
}

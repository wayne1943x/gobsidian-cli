package app

import (
	"context"

	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugin"
)

type App struct {
	registry *plugin.Registry
}

type SyncResponse struct {
	OK      bool                `json:"ok"`
	Command string              `json:"command"`
	Vaults  []plugin.SyncResult `json:"vaults"`
	Errors  []string            `json:"errors"`
}

type StatusResponse struct {
	OK      bool                  `json:"ok"`
	Command string                `json:"command"`
	Vaults  []plugin.StatusResult `json:"vaults"`
	Errors  []string              `json:"errors"`
}

func New(registry *plugin.Registry) *App {
	return &App{registry: registry}
}

func (a *App) Sync(ctx context.Context, cfg config.Config, opts plugin.SyncOptions) SyncResponse {
	out := SyncResponse{OK: true, Command: "sync"}
	for _, target := range cfg.Targets {
		driver, err := a.registry.Get(target.Plugin)
		if err != nil {
			out.OK = false
			out.Errors = append(out.Errors, err.Error())
			out.Vaults = append(out.Vaults, plugin.SyncResult{Vault: target.Name, Plugin: target.Plugin, Error: err.Error()})
			continue
		}
		result, err := driver.Sync(ctx, target, opts)
		result.Plugin = target.Plugin
		if err != nil {
			out.OK = false
			result.Error = err.Error()
			out.Errors = append(out.Errors, err.Error())
		}
		out.Vaults = append(out.Vaults, result)
	}
	return out
}

func (a *App) Status(ctx context.Context, cfg config.Config) StatusResponse {
	out := StatusResponse{OK: true, Command: "status"}
	for _, target := range cfg.Targets {
		driver, err := a.registry.Get(target.Plugin)
		if err != nil {
			out.OK = false
			out.Errors = append(out.Errors, err.Error())
			out.Vaults = append(out.Vaults, plugin.StatusResult{Vault: target.Name, Plugin: target.Plugin, Error: err.Error()})
			continue
		}
		result, err := driver.Status(ctx, target)
		result.Plugin = target.Plugin
		if err != nil {
			out.OK = false
			result.Error = err.Error()
			out.Errors = append(out.Errors, err.Error())
		}
		out.Vaults = append(out.Vaults, result)
	}
	return out
}

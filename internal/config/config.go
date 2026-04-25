package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const CurrentVersion = 1

type Config struct {
	Version int      `json:"version" mapstructure:"version"`
	Targets []Target `json:"targets" mapstructure:"targets"`
}

type Target struct {
	Name     string         `json:"name" mapstructure:"name"`
	Plugin   string         `json:"plugin" mapstructure:"plugin"`
	Vault    VaultConfig    `json:"vault" mapstructure:"vault"`
	State    StateConfig    `json:"state" mapstructure:"state"`
	LiveSync LiveSyncConfig `json:"livesync" mapstructure:"livesync"`
}

type VaultConfig struct {
	Path string `json:"path" mapstructure:"path"`
}

type StateConfig struct {
	Path string `json:"path" mapstructure:"path"`
}

type LiveSyncConfig struct {
	CouchDB CouchDBConfig `json:"couchdb" mapstructure:"couchdb"`
}

type CouchDBConfig struct {
	URL                 string `json:"url" mapstructure:"url"`
	DB                  string `json:"db" mapstructure:"db"`
	Username            string `json:"username" mapstructure:"username"`
	Password            string `json:"password" mapstructure:"password"`
	Passphrase          string `json:"passphrase" mapstructure:"passphrase"`
	PropertyObfuscation bool   `json:"property_obfuscation" mapstructure:"property_obfuscation"`
	BaseDir             string `json:"base_dir" mapstructure:"base_dir"`
	DryRun              bool   `json:"dry_run" mapstructure:"dry_run"`
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType(configType(path))
	var cfg Config
	if err := v.ReadInConfig(); err != nil {
		return Config{}, err
	}
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "mapstructure"
		dc.ErrorUnused = true
	}); err != nil {
		return Config{}, err
	}
	expandEnv(&cfg)
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ResolvePath(explicit string) (string, error) {
	return ResolvePathWithRoots(explicit, "/etc")
}

func ResolvePathWithRoots(explicit, etcRoot string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	candidates := []string{}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".gobsidian", "config.yaml"))
	}
	candidates = append(candidates,
		filepath.Join(etcRoot, "gobsidian", "config.yaml"),
		filepath.Join(mustGetwd(), "config.yaml"),
	)
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("config file not found; searched ~/.gobsidian/config.yaml, %s, and ./config.yaml", filepath.Join(etcRoot, "gobsidian", "config.yaml"))
}

func (c Config) FilterTargets(name string) (Config, error) {
	if name == "" {
		return c, nil
	}
	for _, target := range c.Targets {
		if target.Name == name {
			return Config{Version: c.Version, Targets: []Target{target}}, nil
		}
	}
	return Config{}, fmt.Errorf("vault %q not found", name)
}

func configType(path string) string {
	switch {
	case strings.HasSuffix(path, ".json"):
		return "json"
	case strings.HasSuffix(path, ".toml"):
		return "toml"
	default:
		return "yaml"
	}
}

func validate(cfg *Config) error {
	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	if cfg.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("targets is required")
	}
	seen := map[string]bool{}
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		if target.Name == "" {
			return fmt.Errorf("targets[%d].name is required", i)
		}
		if seen[target.Name] {
			return fmt.Errorf("duplicate target %q", target.Name)
		}
		seen[target.Name] = true
		if target.Plugin == "" {
			return fmt.Errorf("targets[%d].plugin is required", i)
		}
		if target.Plugin == "livesync-couchdb" {
			return fmt.Errorf(`targets[%d].plugin "livesync-couchdb" is no longer supported; use plugin "livesync"`, i)
		}
		if target.Vault.Path == "" {
			return fmt.Errorf("targets[%d].vault.path is required", i)
		}
		if target.Plugin == "livesync" {
			if target.LiveSync.CouchDB.URL == "" {
				return fmt.Errorf("targets[%d].livesync.couchdb.url is required", i)
			}
			if target.LiveSync.CouchDB.DB == "" {
				return fmt.Errorf("targets[%d].livesync.couchdb.db is required", i)
			}
		}
	}
	return nil
}

func expandEnv(cfg *Config) {
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		target.Name = os.ExpandEnv(target.Name)
		target.Plugin = os.ExpandEnv(target.Plugin)
		target.Vault.Path = os.ExpandEnv(target.Vault.Path)
		target.State.Path = os.ExpandEnv(target.State.Path)
		target.LiveSync.CouchDB.URL = os.ExpandEnv(target.LiveSync.CouchDB.URL)
		target.LiveSync.CouchDB.DB = os.ExpandEnv(target.LiveSync.CouchDB.DB)
		target.LiveSync.CouchDB.Username = os.ExpandEnv(target.LiveSync.CouchDB.Username)
		target.LiveSync.CouchDB.Password = os.ExpandEnv(target.LiveSync.CouchDB.Password)
		target.LiveSync.CouchDB.Passphrase = os.ExpandEnv(target.LiveSync.CouchDB.Passphrase)
		target.LiveSync.CouchDB.BaseDir = os.ExpandEnv(target.LiveSync.CouchDB.BaseDir)
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

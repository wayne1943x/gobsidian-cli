# gobsidian-cli

Agent-friendly Obsidian CLI.

`gobsidian-cli` builds the `gobsidian` command. It is designed for agents running
on Linux/server environments that need to synchronize an Obsidian vault, inspect
Markdown notes, and perform small file operations without opening the Obsidian
desktop app.

This project is intentionally CLI-only:

- No daemon
- No scheduler
- No filesystem watcher
- No Docker runtime requirement

## Features

- Sync Obsidian LiveSync CouchDB data to and from a local Markdown vault
- Check sync status for one or more configured vaults
- Search local notes by body text, title/path, and tags
- Read token-friendly slices of note content
- List vault files and folders
- Move notes and update common Obsidian links
- Read, set, and delete frontmatter values

v1 ships one sync plugin:

- `livesync`

Future sync systems such as git should be added as plugins without changing the
local vault operation model. Future LiveSync storage backends such as MinIO
belong under the `livesync` plugin.

## Install

Build from source:

```bash
go build -o gobsidian ./cmd/gobsidian
```

Run directly during development:

```bash
go run ./cmd/gobsidian --help
```

GitHub Actions publishes release binaries from tags named `v*`.

## Configuration

When `--config` is omitted, `gobsidian` searches:

1. `~/.gobsidian/config.yaml`
2. `/etc/gobsidian/config.yaml`
3. `./config.yaml`

Top-level `targets` is the list of vault mappings. Each target selects its own
`plugin`, so different vaults can use different sync drivers in the same config.
v1 supports the `livesync` plugin with the `couchdb` backend under each target's
`livesync` settings.

Minimal example:

```yaml
version: 1

targets:
  - name: personal
    plugin: livesync
    vault:
      path: /vault/obsidian-personal
    state:
      path: /var/lib/gobsidian/state/personal.json
    livesync:
      couchdb:
        url: http://couchdb:5984
        db: obsidian_personal
        username: root
        password: ${COUCHDB_PASSWORD}
        passphrase: ${LIVESYNC_PASSPHRASE}
        property_obfuscation: true
        base_dir: ""
        dry_run: false
```

See [config.example.yaml](config.example.yaml) for required fields, optional
fields, defaults, and a multi-vault example.

## Commands

### Sync And Status

```bash
gobsidian sync --config config.yaml
gobsidian sync --vault personal --config config.yaml
gobsidian sync --vault personal --force-remote --config config.yaml
gobsidian sync --vault personal --force-local --config config.yaml

gobsidian status --config config.yaml
gobsidian status --vault personal --config config.yaml
```

`sync` and `status` run against all configured vaults unless `--vault` is passed.
Use `--force-remote` to rebuild the local vault from LiveSync data without
uploading local changes during that run. Use `--force-local` to make the local
vault authoritative: gobsidian fetches current remote revisions, rewrites local
files to CouchDB, and tombstones remote notes that are absent locally.
The force flags are mutually exclusive.

### Search

```bash
# Body search
gobsidian search "deployment checklist" --vault personal --config config.yaml

# Title/path search
gobsidian search --title "meeting" --vault personal --config config.yaml

# Tag-filtered search; repeated --tag means AND
gobsidian search "draft" --tag project --tag active --vault personal --config config.yaml
```

### Read

```bash
# Full note content to stdout
gobsidian read "notes/example.md" --vault personal --config config.yaml

# Token-friendly reads
gobsidian read "notes/example.md" --head 40 --vault personal --config config.yaml
gobsidian read "notes/example.md" --range 10:80 --vault personal --config config.yaml
gobsidian read "notes/example.md" --max-bytes 12000 --vault personal --config config.yaml
gobsidian read "notes/example.md" --json --vault personal --config config.yaml
```

### List

```bash
gobsidian list --vault personal --config config.yaml
gobsidian list "projects" --recursive --type note --vault personal --config config.yaml
```

### Move

```bash
# Move note and update common Obsidian links by default
gobsidian move "old.md" "archive/new.md" --vault personal --config config.yaml

# Preview link updates without writing
gobsidian move "old.md" "archive/new.md" --dry-run --vault personal --config config.yaml

# Move without link rewriting
gobsidian move "old.md" "archive/new.md" --no-update-links --vault personal --config config.yaml
```

`move` operates on Markdown files directly. It does not mutate `.obsidian` as a
database. Common wiki links and Markdown links are updated by default.

### Frontmatter

```bash
gobsidian frontmatter get "notes/example.md" --vault personal --config config.yaml
gobsidian frontmatter get "notes/example.md" tags --vault personal --config config.yaml
gobsidian frontmatter set "notes/example.md" tags "[project, active]" --vault personal --config config.yaml
gobsidian frontmatter delete "notes/example.md" draft --vault personal --config config.yaml
```

Alias:

```bash
gobsidian fm get "notes/example.md" --vault personal --config config.yaml
```

## Vault Selection

`--vault` selects a configured vault mapping by name.

When exactly one vault is configured, local vault operations may omit `--vault`.
When multiple vaults are configured, local vault operations require `--vault`.

Sync and status commands run against all configured vaults by default:

```bash
gobsidian sync
gobsidian sync --vault personal
gobsidian sync --vault personal --force-remote
gobsidian sync --vault personal --force-local
```

## Output

Most commands print structured JSON to stdout.

`read` prints raw Markdown by default. Pass `--json` to get structured output.

Logs and errors go to stderr. If any selected `sync` or `status` vault fails, the
command exits non-zero and still prints structured JSON.

## Development

Run checks from the repository root of `gobsidian-cli`:

```bash
go test ./...
go vet ./...
go build -o /tmp/gobsidian-check ./cmd/gobsidian
```

## Release

GitHub Actions publishes releases from tags named `v*`.

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow runs tests, runs `go vet`, builds Linux/macOS binaries for
amd64 and arm64, and uploads tarballs plus `checksums.txt`.

## Contributing

Contributions are welcome. Please keep changes aligned with the project scope:

- Preserve the CLI-only model; do not add daemon, scheduler, or watcher behavior
  to the core app.
- Keep sync backends behind the plugin interface.
- Keep local vault operations safe for real Obsidian vaults.
- Do not allow path traversal outside the configured vault root.
- Avoid writing personal vault names, private identifiers, or secrets into tests,
  examples, comments, or docs.
- Add focused tests for behavior changes.

Before opening a pull request, run:

```bash
go test ./...
go vet ./...
```

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).

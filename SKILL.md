---
name: gobsidian-cli
description: "Use this skill when an agent needs to work with an Obsidian vault through the gobsidian CLI: sync LiveSync CouchDB data, check status, search notes, read token-friendly note slices, list files, move notes, or edit frontmatter."
---

# gobsidian-cli

Use `gobsidian` to operate on real Obsidian Markdown vaults from a shell. Prefer
this CLI over ad hoc `grep`, `find`, or manual YAML edits when the task involves
Obsidian notes, tags, frontmatter, or LiveSync-backed vaults.

## Mental Model

`gobsidian` has two layers:

- Sync commands: `sync`, `status`
- Local vault commands: `search`, `read`, `list`, `move`, `frontmatter`

Local vault commands operate on Markdown files in the configured vault path. They
do not contact CouchDB and do not automatically sync.

Sync commands use the configured plugin. v1 supports `livesync` with the
`livesync.couchdb` backend.

## Config

Use `--config` when a config path is known:

```bash
gobsidian status --config config.yaml
```

Without `--config`, the CLI searches:

1. `~/.gobsidian/config.yaml`
2. `/etc/gobsidian/config.yaml`
3. `./config.yaml`

Use `--vault <name>` when multiple vaults are configured. If only one vault is
configured, local vault commands may omit `--vault`.

## Common Workflow

Before reading or editing notes backed by LiveSync, sync first:

```bash
gobsidian sync --vault personal --config config.yaml
```

Find candidate notes with `search` or `list`, then read only the needed content:

```bash
gobsidian search "deployment checklist" --tag project --vault personal --config config.yaml
gobsidian read "projects/deployment.md" --head 80 --vault personal --config config.yaml
gobsidian read "projects/deployment.md" --range 40:120 --vault personal --config config.yaml
```

After edits, push changes back:

```bash
gobsidian sync --vault personal --config config.yaml
```

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

`sync` and `status` target all configured vaults unless `--vault` is passed.
Use `--force-remote` when LiveSync should overwrite local files for that run.
Use `--force-local` when the local vault should overwrite CouchDB, including
tombstoning remote notes that are absent locally. Do not combine them.

### Search

```bash
gobsidian search "term" --vault personal --config config.yaml
gobsidian search --title "meeting" --vault personal --config config.yaml
gobsidian search "draft" --tag project --tag active --vault personal --config config.yaml
```

Repeated `--tag` filters are AND filters. Use search before broad reads to save
tokens.

### Read

```bash
gobsidian read "notes/example.md" --vault personal --config config.yaml
gobsidian read "notes/example.md" --head 40 --vault personal --config config.yaml
gobsidian read "notes/example.md" --tail 40 --vault personal --config config.yaml
gobsidian read "notes/example.md" --range 10:80 --vault personal --config config.yaml
gobsidian read "notes/example.md" --max-bytes 12000 --vault personal --config config.yaml
gobsidian read "notes/example.md" --json --vault personal --config config.yaml
```

`read` prints raw Markdown by default. Use `--json` when metadata, path, tags, or
truncation status matters.

### List

```bash
gobsidian list --vault personal --config config.yaml
gobsidian list "projects" --recursive --type note --vault personal --config config.yaml
```

Use `list` for structure discovery. It respects configured Obsidian ignore
filters where applicable.

### Move

```bash
gobsidian move "old.md" "archive/new.md" --vault personal --config config.yaml
gobsidian move "old.md" "archive/new.md" --dry-run --vault personal --config config.yaml
gobsidian move "old.md" "archive/new.md" --no-update-links --vault personal --config config.yaml
```

`move` works on Markdown files directly and updates common Obsidian wiki links
and Markdown links by default. Use `--dry-run` before large reorganizations.

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

## Output Rules

Most commands print JSON to stdout.

`read` prints raw Markdown unless `--json` is passed.

Errors and logs go to stderr. If `sync` or `status` partially fails, inspect the
JSON response even when the process exits non-zero.

## Safety Rules

- Do not edit files outside the configured vault root.
- Do not treat `.obsidian` as a note database.
- Prefer `gobsidian frontmatter` over manual YAML edits.
- Prefer `gobsidian move --dry-run` before reorganizing many notes.
- Run `gobsidian sync` after local modifications when the user expects changes
  to reach Obsidian on other devices.

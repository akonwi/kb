# Migrating from QMD

`kb` can import QMD's YAML configuration directly; QMD does not need to be installed or running.

```sh
kb config import-qmd
```

Without a file argument, `kb` first searches the current directory and its parents for `.qmd/index.yaml` or `.qmd/index.yml`. If none is found, it checks QMD's global configuration location:

1. `$QMD_CONFIG_DIR/index.yml`
2. `$XDG_CONFIG_HOME/qmd/index.yml`
3. `~/.config/qmd/index.yml`

A specific or non-default QMD index can be supplied explicitly:

```sh
kb config import-qmd ~/.config/qmd/work.yml
```

The command imports configuration without reading files or running hooks. Build the index as a separate, reviewable step:

```sh
kb config import-qmd
kb collection list
kb update
```

For a trusted configuration, `--update` performs the second step immediately:

```sh
kb config import-qmd --update
```

Configuration commits before indexing begins, so `--update` is intentionally explicit: if a later collection read fails, the imported configuration remains and a subsequent `kb update` resumes reconciliation.

Normal imports merge by collection name. `--replace` additionally removes existing `kb` collections absent from the QMD configuration:

```sh
kb config import-qmd ~/.config/qmd/index.yml --replace
```

If QMD contains `includeByDefault=false` collections, `--replace` requires `--include-nondefault` so those collections cannot be accidentally omitted during replacement.

## Mapping

| QMD setting | `kb` setting |
|---|---|
| Collection map key | Collection name |
| `path` | Canonical collection root |
| `pattern` | Glob pattern; defaults to `**/*.md` |
| `ignore` | Ignore patterns, plus QMD's defaults for `node_modules`, `.git`, `.cache`, `vendor`, `dist`, and `build` |
| Collection `context` | Path-prefix contexts |
| `global_context` | Root fallback context copied into each imported collection |

For project-local `.qmd` files, relative collection paths are resolved from the project directory containing `.qmd`. For explicit/global files, they are resolved from the configuration file's directory.

QMD collection-level root context overrides `global_context`. `kb` uses path-component boundaries when matching context prefixes, rather than matching arbitrary string prefixes.

## Settings intentionally not migrated

- Embedding, reranking, and generation model settings
- Editor URI settings
- Vector data and QMD's generated SQLite index
- `includeByDefault=false` query-default behavior; these collections are skipped unless `--include-nondefault` is supplied
- Collection `update` shell hooks

Update hooks are neither imported nor executed for security. Auto-detected project-local configuration is configuration-only unless `--update` is explicitly supplied. The command prints warnings for unsupported settings. QMD glob syntax that has no equivalent in `kb`—including comma unions such as `**/*.md,**/*.txt`, extglobs, brace ranges, leading negation, and POSIX character classes—is rejected rather than silently narrowed. Split those masks into compatible collections before importing. `kb` rebuilds lexical FTS5 data from the original Markdown files instead of trusting or converting QMD's generated index.

After migration, verify the result before removing QMD:

```sh
kb collection list
kb status
kb doctor
kb search "representative query"
kb config export ~/kb-config.json
```

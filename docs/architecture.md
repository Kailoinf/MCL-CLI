# Architecture

## Layers

```
CLI (main.go, flags.go, commands.go, launch_cmd.go, app.go)
 │  argv parsing, i18n setup, prompting, process spawn
 ├─ internal/mcv      version metadata and file installation
 ├─ internal/java     runtime discovery / installation
 ├─ internal/launch   command line assembly
 ├─ internal/auth     Microsoft → Xbox Live → Minecraft
 ├─ internal/dl       transport: candidate lists, retries, checksums, progress
 ├─ internal/mirror   URL mapping (BMCLAPI ↔ official)
 ├─ internal/store    mcli.db persistence
 ├─ internal/config   config table schema + validation
 ├─ internal/secret   token encryption
 ├─ internal/paths    directory layout
 └─ internal/i18n     bilingual string catalogs
```

Dependencies only ever point downwards: `mcv` uses `dl`/`mirror`/`i18n`, never
the CLI packages. `i18n` depends on nothing.

## Launch pipeline

```
mcli launch <instance>
  1. resolve instance → version id → per-instance game dir
  2. mcv.ManifestStore.Get(force=false)
        cached in mcli.db (manifest_cache), refreshed when older than
        manifest_ttl_hours (silently when manifest_auto_refresh is on)
  3. mcv.Loader.Lookup(versionID, entry.URL)
        versions/<id>/<id>.json, downloaded if missing, then inheritsFrom
        followed recursively (max depth 5)
  4. Version.Effective()  → merged child/parent document
  5. java.RequiredMajor() → component/major from javaVersion, table as fallback
     java.ScanRuntimes() → Pick() (exact major, else smallest newer)
     no local runtime → offer to install one (java.Installer)
  6. mcv.Installer.Ensure(effective)
        client jar, libraries, natives jars, asset index, asset objects,
        logging config; plan first (count/size), one aggregate Y/N prompt,
        then a 6-worker download pool
  7. auth: stored session, refreshed when it expires within 5 minutes
  8. launch.Prep() → memory flags + JSON jvm/game args + placeholders + classpath
  9. store last launch in mcli.db (for --last), spawn java, stream the game's
     stdout/stderr into ~/.mcl-cli/logs/game-<instance>-<timestamp>.log
```

## Instance isolation

* Version files live in `versions/<version-id>/` and are shared by every
  instance that uses that version.
* Game data (saves, options.txt, resourcepacks) lives in `versions/<instance>/`
  and is passed to the game as `--gameDir`.
* `libraries/` and `assets/` are shared by all versions, exactly like the
  official launcher.

## The three documents in ~/.mcl-cli

All TOML, parsed and written by `internal/toml` (no dependency), always written
atomically (`.tmp` + rename):

```
mcli.toml       [config]        ← the config table (see internal/config)
                hand-editable; `config set` patches one line in place, so
                comments, ordering and extra keys survive; no secrets, so it is
                portable between machines

state.toml      db_version      machine-owned (0600)
                accounts{}      ← keyed by Minecraft UUID; each entry carries
                                  its own token_salt plus AES-GCM encrypted tokens
                instances{}     ← name → {version, per-instance java/xmx/xms/args}
                manifest_cache{}← which source served the manifest, when, and the
                                  path of the cached body

last.toml       the previous launch (instance, version, java, xmx/xms, window
                size, jvm/game args, game dir) — tiny on purpose: `launch --last`
                never has to read mcli.toml or state.toml

cache/version_manifest_v2.json   the raw manifest body, exactly as downloaded
```

## Error handling style

Errors are returned, not panicked. The CLI translates them once at the top
(`App.fatal` → one line on stderr/stdout, exit code 1; usage problems → exit 2).
`i18n.T` is used at the point where a message is *formatted* so wrapped errors
stay readable in the user's language.

## Download policy

`dl.Policy` carries all transport behaviour: retry count, timeout, "large file"
threshold, confirmation callback, logger. Stalls are detected by a watchdog
reader that cancels a request when no bytes arrive for 20 seconds (the BMCLAPI
redirect nodes do stall). Verification is SHA-1 + size; the destination is only
written after verification succeeds, so a killed download cannot leave a
poisoned file in the shared cache.
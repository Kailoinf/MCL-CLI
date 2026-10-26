# Architecture

## Layers

```
CLI (main.go, flags.go, commands.go, instance_cmd.go, instance_import.go,
     account_cmd.go, launch_cmd.go, app.go, table.go)
 │  argv parsing, i18n setup, prompting, process spawn
 ├─ internal/mcv      version metadata and file installation
 ├─ internal/java     runtime discovery / installation
 ├─ internal/launch   command line assembly
 ├─ internal/auth     Microsoft → Xbox Live → Minecraft
 ├─ internal/dl       transport: candidate lists, retries, checksums, progress,
 │                    source health
 ├─ internal/mirror   URL mapping (BMCLAPI ↔ official)
 ├─ internal/store    the TOML documents (config, state, credentials, last)
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
        cached in state.toml (manifest_cache), refreshed when older than
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
  9. store last launch in last.toml (for --last), spawn java, stream the game's
     stdout/stderr into ~/.mcl-cli/logs/game-<instance>-<timestamp>.log
```

## Instance isolation

* Version files live in `versions/<version-id>/` and are shared by every
  instance that uses that version.
* Game data (saves, options.txt, resourcepacks) lives in `versions/<instance>/`
  and is passed to the game as `--gameDir`.
* `libraries/` and `assets/` are shared by all versions, exactly like the
  official launcher.

## The five documents in ~/.mcl-cli

All TOML, parsed and written by `internal/toml` (no dependency), always written
atomically (`.tmp` + rename):

```
mcli.toml       [config]        ← the config table (see internal/config)
                hand-editable; `config set` patches one line in place, so
                comments, ordering and extra keys survive; no secrets, so it is
                portable between machines

state.toml      state_version      machine-owned, 0600, no secrets → portable
                accounts{}      ← keyed by Minecraft UUID: name, xuid, label,
                                  active flag, timestamps (identity only)
                instances{}     ← name → {version, per-instance java/xmx/xms/args}
                manifest_cache{}← which source served the manifest, when, and the
                                  path of the cached body

refresh.toml    tokens{}        ← keyed by account UUID: the long-lived refresh
                (0600)            token plus the salt its key derives from.
                                  Rewritten at login (not on every token refresh)

session.toml    sessions{}      ← keyed by account UUID: the short-lived
                (0600)            Minecraft access token and its expiry

last.toml       the previous launch (instance, version, java, xmx/xms, window
                size, jvm/game args, game dir) — tiny on purpose: `launch --last`
                never has to read any other document

cache/version_manifest_v2.json   the raw manifest body, exactly as downloaded
```

`state_version` is 1. There is exactly one shape and no migration path: mcli has
no users on an older one, so a file from anywhere else is either this shape or
not supported at all. A file written by a *newer* mcli keeps its number and the
CLI warns instead of garbling it.

## Taking over an existing .minecraft

`mcli instance scan` lists every `versions/<id>/` directory on disk, and
`mcli instance import <id>` registers one as an instance. Nothing is moved: the
version files stay where they are, and because mcli uses `versions/<instance>/`
as the game directory, a profile directory laid out by another launcher keeps its
saves, options and mods.

Two properties of other launchers' output matter here:

* **Flattened profiles.** PCL writes a modded profile as a single version JSON
  with `arguments`, `libraries`, `downloads.client` and `javaVersion` all copied
  in, and no `inheritsFrom`. Such a document works as-is. When a profile *does*
  inherit, `mcv.ClientJarTarget` decides which client jar it runs: an explicit
  `jar` field first, then a jar already sitting in the version's own directory
  (PCL and HMCL copy the vanilla jar into profile directories), then the parent.
* **Natives subdirectories.** Modern JSONs (and every flattened profile) point
  JNA, LWJGL and Netty at their own directory under `natives/` and give
  `java.library.path` a separate one, because those libraries unpack their native
  code from the classpath at run time. `mcv.Version.NativesTargets` reads those
  arguments, creates the directories, and `nativeDirsFor` unpacks each natives jar
  into the base directory plus the family subdirectory the version asks for.

## Diagnosis: doctor and repair

`mcv.Plan` is the single description of "which files does this version need":
the version's own files (client jar, libraries, asset index, log config), the
natives and the directories their arguments name, and every object of the asset
index. `Ensure` (install), `doctor` (report) and `repair` (fetch) all read it, so
they cannot disagree about what a version needs.

* `doctor` works with a dry installer and a dry loader, so it never writes — not
  even a directory. `--deep` hashes asset objects instead of trusting their size.
* `repair` fetches only what `Plan.Check` reports, then runs `Ensure` once to
  confirm the result and extract natives, and finally checks the runtime. `--force`
  deletes the planned files first so the downloader really re-fetches them.
* Missing kinds stay apart — absent, wrong size, wrong hash — and size is compared
  before the hash, so a truncated file is reported as truncated rather than as a
  hash mismatch.

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

`dl.SourceHealth` is shared by every policy in one run: three failures of the
same source demote it to the end of the candidate list for the rest of the run
(never removed — a network that can only reach the mirror still gets its files).
That is what keeps a 700-file Java runtime install from paying a stall timeout
per file.
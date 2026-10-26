# Design decisions

Each entry: the decision, why, and what it costs. Written to be argued with.

## TOML instead of a database — and five files instead of one

The requirement asks for "config in `.mcl-cli/mcli.db`, with a `config` table".
A file called `.db` that is not a database is a trap for anyone who later opens
it with `sqlite3`; real SQLite would mean a heavy cgo-free driver
(`modernc.org/sqlite`) or cgo, against this project's zero-dependency rule.
TOML satisfies both constraints: readable, format-accurate file names, and a
parser/writer we can own (`internal/toml`, ~1000 lines including tests).

The documents are split on ownership and lifecycle:

* `mcli.toml` — the user's. It must survive an editor session, so writes are
  line-level patches (`toml.SetKeyInSection`) that keep comments, ordering and
  unknown keys intact instead of re-serialising the struct.
* `state.toml` — mcli's. Accounts (identity only), instances and the manifest
  pointer change on every other command, so it is rewritten wholesale. It holds
  no secrets, which means it is safe to copy, sync or paste when asking for help.
* `refresh.toml` / `session.toml` — the credentials, deliberately in two files.
  A Microsoft refresh token is long-lived and worth protecting; a Minecraft
  access token dies in a few hours. Splitting them means (a) the file worth
  backing up like a password changes rarely instead of hourly, (b) deleting
  `session.toml` forces a fresh Minecraft token without touching the login, and
  (c) a token leak has an obvious blast radius — the short-lived file alone is
  worthless. Cost: one more file to document and one more `SaveTokens` call at
  the end of a login. Worth it for the security-shaped separation.
* `last.toml` — the previous launch. `mcli launch --last` should not have to
  parse a configuration to replay flags, so those ~15 values get their own tiny
  file.

Migration is one-way and quiet: a legacy JSON `mcli.db` (db_version 1) is
imported, a v2 `state.toml` has its inline tokens moved into the two credential
files, and the moved keys disappear from `state.toml`. `db_version` records the
shape so a newer mcli's file is reported instead of misread.

Cost: five files instead of one, and a `--config <path>` override only moves the
config (the rest derive from its directory). Both are documented in the README.

## One salt per account, not one per machine

Encryption keys are `PBKDF2-SHA256(machine identity, salt)`; the salt lives next
to the account it protects. That keeps the blast radius of a stolen file small
(the file alone is useless elsewhere) *and* keeps everything else portable: an
installed game, the config and the instance list work on another computer, only
the login has to happen again. A machine-wide salt would have made the whole
state file non-transferable for no extra benefit.

Legacy JSON `mcli.db` files (db_version 1) are imported once: the machine-wide
salt is copied onto each imported account so existing tokens keep decrypting,
and the old file is left untouched for the user to delete.

## Instance game directories live under `versions/<instance>/`

The spec's tree shows instance data under `.minecraft/version/{instance-name}/`.
Keeping that shape, but preserving the official launcher's compatibility, means:
version *files* in `versions/<version-id>/` (so the official launcher can also
see them) and per-instance *game data* in `versions/<instance>/`, passed to the
JVM as `--gameDir`. `versions_dir` is configurable (default `versions`), and the
README documents the layout explicitly.

Cost: a version directory and an instance directory can collide in name if the
user names an instance exactly like a version id. The launcher warns/overrides
through the normal instance lookup in that case.

## Taking over installs made by other launchers

A launcher that cannot see the `.minecraft` it lands on is not usable, so
`instance scan` / `instance import` adopt existing version directories instead of
asking the user to reinstall 700 MB. Two consequences shaped the code:

* Instance names accept spaces and CJK, because `26.3-Fabric 0.20.1` is the name
  a real PCL profile has on disk. Path separators, Windows-forbidden characters
  and reserved device names are still refused, and so is a trailing dot.
* Natives are unpacked where the version's own arguments point
  (`natives/jna`, `natives/lwjgl`, `natives/netty`, `natives/java`), not just in
  the base directory. Modern Minecraft libraries (LWJGL, JNA, Netty) extract
  themselves from the classpath at run time, but only into a directory that the
  JSON names — so the launcher must create exactly those directories and put the
  pre-extracted files where a legacy `java.library.path` expects them.

mcli still does not *install* Fabric, Forge or any other loader: it runs what
already exists. Mod loading stays out of scope, which is different from refusing
to launch a modded install.

## A failing mirror is demoted, not disabled

The mirror is tried first for every file, which is right when it works and
expensive when the redirector stalls: a Java runtime is ~700 small files and each
stall costs a watchdog timeout. `dl.SourceHealth` counts consecutive failures per
source and, after three, moves the official source to the front for the rest of
the run. The mirror stays in the list, because a network that can only reach the
mirror still has to install.

Cost: one heuristic with a threshold nobody can prove is optimal. It is logged
once when it triggers ("… keeps failing; the other sources are tried first"),
so a surprising fallback is visible rather than silent.

## Multiple accounts, one active

Accounts are keyed by Minecraft UUID, never by name: the same player can rename,
and two accounts can share a name across regions. `mcli account list` shows
everything stored, `account use` switches, `launch --account <name|uuid|label>`
overrides for a single run, and `login --no-switch` adds an account without
moving the default away from the one in use. Identity and credentials are in
different files, so the account list always renders even when a token file is
missing or was encrypted on another machine.

## Java runtimes come from Mojang's product manifest

`launchermeta.mojang.com/v1/products/java-runtime/<hash>/all.json` is what the
official launcher itself installs, it is mirrored by BMCLAPI, and the version
JSON already names the component it wants (`java-runtime-delta` etc.). That is
strictly better than guessing JDK builds: the runtime matches what Mojang ships
with the game.

Cost: ~430 small files per runtime (~115 MiB) instead of one archive. The
downloader's worker pool handles it, and the whole batch is confirmed once.
Adoptium's tarball remains as a fallback for platforms Mojang does not publish
for (e.g. Linux arm64).

## Prefer local Java, never install silently

Matching is "exact major, else smallest newer". Nothing is downloaded without an
explicit Y/N (or `--yes`). `java_path` pins a runtime but is still checked
against the version's requirement, so a pin cannot silently launch the game on a
too-old JVM.

## Every download is an ordered candidate list

The BMCLAPI v2 endpoint redirects some requests to a `*.sakura.sld.tw` node that
is frequently slow; official Mojang hosts are throttled in some networks and
fast in others. Rather than a global "mirror on/off" switch, each file carries
its candidates (mirror first, official last) and the downloader walks them,
retrying the whole list. A stalled transfer is aborted after 20 s and the next
candidate is tried.

Cost: on a bad day a single file may be attempted several times, which shows up
as `WARN` lines in the log. That is deliberate: silence about a degrading mirror
would be worse.

## Assets: mirror the index, then objects by hash

BMCLAPI does not expose `/assets/indexes/<id>.json` (404) but mirrors
`piston-meta` paths, so the index is fetched through `/v1/packages/<sha1>/<id>.json`
and the objects through `/assets/<first2>/<hash>`. Objects are content-addressed,
which makes "already present" cheap to detect and makes a shared cache safe
between instances and launchers.

## AES-GCM + machine-derived key instead of an OS keychain

The spec asks for tokens encrypted with a key derived from local device info,
without depending on the system keychain. PBKDF2-SHA256 (200k iterations) over
`machine-id` + product UUID + hostname + user, salted with random bytes stored
in `mcli.db`. Consequences are documented in the README and shown in errors:
a `mcli.db` copied to another machine cannot be decrypted, and stealing the file
alone does not yield the refresh token.

## i18n keyed by dotted ids, English as the source of truth

Translations are keyed (`dl.sha_mismatch`), never by English text, so wording can
change freely. Two tests enforce the contract: every key used anywhere in the
repository must exist (`TestSourceKeysExist`), and the two catalogs must have
identical keys and identical printf verbs (`TestCatalogsInSync`,
`TestFormatVerbsMatch`). Language resolution: `--lang` → `mcli.db:language` →
`MCLI_LANG` → `LC_ALL`/`LC_MESSAGES`/`LANG` → English.

## Flags only, no interactive wizard

Every action is expressible as a command plus flags, which is what makes the
tool scriptable from cron or a CI runner. The only interactive elements are
Y/N confirmations for large downloads, and `--yes` disables them. `--dry-run` on
launch prints the exact java command line without touching the game files.

## Logs: stdout for results, stderr for diagnostics

Results (tables, paths, statuses) go to stdout so they can be piped; leveled
diagnostics go to stderr and to the daily log file. The game's own output goes
to `logs/game-<instance>-<timestamp>.log`, never interleaved with launcher
output.
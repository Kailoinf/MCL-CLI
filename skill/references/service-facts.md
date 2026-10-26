# Service facts

Endpoints and shapes that mcli depends on. These are the facts behind the code; the
running status (what was verified when, what broke) belongs in `docs/status.md`.

## BMCLAPI (the mirror)

- `bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json` may answer **302 to a
  `*.sakura.sld.tw` node that stalls**. Keep the official `piston-meta.mojang.com`
  copy as a fallback and never remove `dl`'s stall watchdog (20 s).
- Paths that work: `/version/<id>/json`, `/version/<id>/client`,
  `/maven/<maven path>`, `/assets/<first2>/<hash>`,
  `/v1/packages/<sha1>/<file>.json`, `/v1/products/java-runtime/<hash>/all.json`.
- `/assets/indexes/<id>.json` is **404** and `/java/list` is **403** — do not build on
  either.
- Libraries on other hosts (`maven.fabricmc.net`, `maven.minecraftforge.net`) pass
  through untouched; only Mojang's hosts are mirrored.
- Every download is therefore a candidate list (mirror first, official second), and a
  `SourceHealth` record downgrades a source that misbehaves.

## Version JSON

- Modern documents carry `arguments.jvm`/`arguments.game` (a string or
  `{rules, value}`), `javaVersion.{component,majorVersion}`, and natives as separate
  libraries named `…:natives-linux`.
- Legacy ones carry `minecraftArguments`, a `natives` map and `downloads.classifiers`.
- Both shapes are covered by tests; keep both working.
- A profile written by another launcher may have no `inheritsFrom` and put the game
  version in `clientVersion`, with the loader named only by its libraries.

## Java runtimes

- Mojang's java-runtime product manifest is the source (`javaVersion.component` names
  the runtime); BMCLAPI mirrors it, keyed by platform then component.
- Components: `jre-legacy`=8, `java-runtime-alpha`=16, `-beta`/`-gamma`=17,
  `-delta`=21, `-epsilon`=25. Current releases (`26.x`) need Java 25;
  `javaVersion.majorVersion` wins over any id-based table.
- mcli writes a `.mcli-ok` marker only after `java -version` passes, so a half-extracted
  runtime is never treated as installed.

## Microsoft and Xbox Live

- Browser flow: authorize at `login.live.com/oauth20_authorize.srf`
  (`response_type=code`, scope `XboxLive.signin offline_access`,
  `prompt=select_account`), a loopback server on ports 29116-29126 answering
  `/authentication-response`, then exchange at `login.live.com/oauth20_token.srf`.
  No secret and no PKCE. Refresh goes to the same host and sends **no scope**.
- Device flow: `login.microsoftonline.com/consumers/oauth2/v2.0/devicecode` with scope
  `service::user.auth.xboxlive::MBI_SSL offline_access`.
- The reference launcher's client id `288ec5dd-6736-4d4b-9b96-30e083a8cad2` works for
  the browser flow but is **not enabled for device codes** (`AADSTS9002346`);
  `00000000402b5328` no longer exists (`AADSTS700016`). `auth_client_id` is
  configurable for that reason.
- Chain: `user.auth.xboxlive.com/user/authenticate` →
  `xsts.auth.xboxlive.com/xsts/authorize` →
  `api.minecraftservices.com/authentication/login_with_xbox` → `/minecraft/profile`.
- XSTS error codes carry meaning: `2148916233` no Xbox account, `2148916235` region
  blocked, `2148916238` child account. They are bigger than a 32-bit `int`, which is
  why they are `int64` constants.
- Smoke test without an account: `./mcli login --no-browser --timeout 25`, read the
  port from the printed URL, then `curl
  "http://localhost:<port>/authentication-response?code=FAKE"` — the callback page
  renders and Microsoft answers `invalid_grant`, which proves the wiring.

## Fabric, Modrinth

- Fabric meta: `https://meta.fabricmc.net`, loader list
  `/v2/versions/loader/<game>`, profile
  `/v2/versions/loader/<game>/<loader>/profile/json`. The profile's libraries have
  `name`/`url`/`sha1` and no `downloads` block.
- Modrinth needs no API key: search `api.modrinth.com/v2/search`, a project
  `/v2/project/<slug>`, its versions `/v2/project/<slug>/version?game_versions=[...]`,
  files on `cdn.modrinth.com` with declared `sha1`/`sha512`.
- CurseForge needs a per-user API key: not wired, and it is the remaining item in the
  plan.

## Minecraft server

- The jar comes from the version JSON's `downloads.server`.
- The console is stdin/stdout; a detached server's stdin is a FIFO and its output goes
  to `logs/console.log`, while the server's own logger writes `logs/latest.log`.
- `Done (Ns)!` in that log is the readiness signal, and `Stopping server` /
  `Saving worlds` are the proof of a graceful stop.
- The EULA URL is `https://aka.ms/MinecraftEULA`; mcli prints it and asks, never
  accepts it for the user.

## CurseForge (needs a per-user key)

* Base `https://api.curseforge.com`; the key travels in the `x-api-key` header and is
  required by every endpoint. `GET /v1/mods/search?gameId=432&classId=…&searchFilter=…&gameVersion=…&modLoaderType=…&index=…&pageSize=…`,
  `GET /v1/mods/{id}/files`, `GET /v1/mods/{id}`. A refused key is 401/403 — do not
  report it as a network failure.
* gameId Minecraft = 432. Class ids: mods 6, resource packs 12, modpacks 4471, shader
  packs 6552, data packs 6945, worlds 17. modLoaderType: forge 1, fabric 4, quilt 5,
  neoforge 6. Hash algo 1 = SHA-1, 2 = MD5.
* A file's `gameVersions` mixes the game version with loader names ("1.21.4", "Fabric"),
  so both filters are scans of the same list.
* The key is a credential: store it in `mcli.toml`, mask it in every read path
  (`config.Secret`), never log it, never print it.
* Verified as far as it can be without a working key: a wrong key reaches the API and
  comes back 403, which the CLI turns into "the key was rejected".

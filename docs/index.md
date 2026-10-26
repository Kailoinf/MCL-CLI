---
layout: home

hero:
  name: mcli
  text: "Minecraft: Java Edition, from the command line"
  tagline: "One Go binary, no dependencies. Installs, launches, repairs and serves — with a real Microsoft account and a mirror that works where the official hosts are slow."
  actions:
    - theme: brand
      text: Get started
      link: /guide
    - theme: alt
      text: Commands
      link: /commands
    - theme: alt
      text: GitHub
      link: https://github.com/Kailoinf/MCL-CLI

features:
  - title: A launcher, not a wrapper
    details: "It resolves the version manifest, plans the exact file set (client, libraries, natives, assets), downloads with retries and a stall watchdog, extracts natives and assembles the java command line itself."
  - title: Instances
    details: "Named instances with their own game directory, memory and arguments. Adopt what another launcher already installed — PCL, HMCL, the official one — without re-downloading a gigabyte."
  - title: Several accounts
    details: "Microsoft login through the browser flow or the device flow. Refresh tokens and session tokens live in two separately encrypted files, and `account list` shows who is logged in."
  - title: Updating
    details: "`mcli update` moves an instance to a newer game version — the newest release by default — while keeping its loader, its memory, its arguments, its mods and its worlds. The version it left behind keeps its files: another instance may be running them."
  - title: Doctor and repair
    details: "`mcli doctor` says what is missing or damaged and exits non-zero when something is wrong; `mcli repair` fetches exactly that list and nothing else."
  - title: Servers
    details: "`mcli server` installs a server jar, writes eula.txt and server.properties, runs it attached or detached, and lets you attach to a running console."
  - title: Mods and resource packs
    details: "`mcli search` queries Modrinth — one query, one page — and `mcli add` puts the matching file into the instance's mods/, resourcepacks/ or shaderpacks/ with its SHA-1 checked. A vanilla instance is told to use a loader instead of quietly getting a jar it cannot load."
  - title: Fabric
    details: "The loader is a choice on the version: `--loader fabric` on `create` and `instance set` fetches the profile Fabric publishes, so instances with mods work — including the ones another launcher set up."
---

## What it looks like

```sh
mcli login                                # Microsoft account, browser flow
mcli create vanilla --version latest       # an instance of the current release
mcli launch vanilla                        # installs what is missing, then starts
mcli doctor --all                          # is anything missing or damaged?
mcli server create survival --version latest --eula
mcli server start survival --detach
mcli server attach survival                # its console, without stopping it
mcli create modded --version latest --loader fabric   # or --loader vanilla
```

Everything is a flag; nothing is a wizard. See [the guide](/guide) for the full
walkthrough, [commands](/commands) for the reference, and
[architecture](/architecture) / [design decisions](/design-decisions) for how it
works inside. `docs/status.md` — [here](/status) — is the live state of the
project: what has been verified with real commands, what is left, and what broke
along the way.

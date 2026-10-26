# AGENTS.md — working on mcli

Working state — the current version, what has been verified with real commands,
service facts, quirks and loose ends — lives in **`docs/status.md`**. Read that
first, and update it in the same commit as the work.

This file holds only the rules that stay true. No code detail: it is protected,
and the maintainer approves every edit.

## Rules

1. No hardcoded user-visible text. Every string goes through `i18n.T("key")`, and
   the key exists in **both** `internal/i18n/en.go` and `internal/i18n/zh.go` —
   the i18n tests fail otherwise.
2. Zero third-party dependencies: the standard library only. A new dependency
   needs the maintainer's approval first.
3. Never print secrets — show paths, lengths or counts instead.
4. `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green before every
   commit; commit and push in small working steps rather than batches.
5. Docs ship with the code: README.md, README.zh-CN.md and `docs/*` must match
   what was just written.
6. `mcli doctor` never writes, and `mcli repair` fixes exactly what doctor
   reported — both come from one shared plan, so they cannot disagree.
7. Anything that can run to thousands of lines on a real machine is capped or
   summarised, never one line per file.
8. Never test against a real home or someone's `.minecraft`: point `MCLI_HOME`
   and `MCLI_MINECRAFT` at a scratch directory and work on a copy.
9. Do not launch the game here — this machine has no display. Judge a launch from
   `--dry-run` output and report what the command line actually contains.
# Contributing

Thanks for helping. Bug reports, fixes and new audit checks are all welcome.

- **Bugs:** open an issue with what you did, what you expected and what happened. Leave out API keys and passwords.
- **Pull requests:** keep them focused, add a test for a fix where you can, and run `make test` first.
- **New audit checks** live in `internal/crawler/crawler.go` (with a plain-language name and fix in `web/src/pages/Audit.tsx`).

## Layout

| Path | What it is |
|---|---|
| `cmd/datafluxor-seo` | the program |
| `internal/server` | HTTP API and the embedded web app |
| `internal/crawler` | site audit crawler and checks |
| `internal/ranks` | rank tracking and the daily schedule |
| `internal/dfapi` | DataFluxor API client |
| `internal/gsc` | Google Search Console client |
| `internal/store` | SQLite database |
| `web` | React + TypeScript web app (Vite) |

By contributing you agree that your contribution is licensed under the AGPL-3.0, like the rest of the project.

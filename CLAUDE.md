# Grabarr: notes for Claude

## Project
Copies finished downloads from the Whatbox seedbox to the NAS (millions). Every few minutes it scans watched folders on the seedbox over SSH, queues a job per new file, copies with rsync under a bandwidth and disk-space gatekeeper, extracts archives, tells Radarr/Sonarr when a file lands, and alerts on failures via Pushover. A SvelteKit web app shows files, jobs and settings.

## Task tracking: Kan
Work is tracked on the Kan board **"Grabarr"** at https://tasks.dppeppel.me (board `zvce5q4bgsf5`, labels P1/P2, `ui`, `books`, `bug`, `standards`), through the `kan` MCP server's tools.
- Lists: To-Do `pswbz0nkgvxw`, Doing `cvr86iosw8ua`, Done `qo2vq8p5hwj3`. A card is the source of truth for its scope and its "Done =" check.
- Move a card to Doing when you start it, comment when you deploy and when it's verified, then move it to Done. Add cards for new scope (not TODO files).

## Wiki
The running app is documented on the Outline page **Media › Acquisition › Grabarr** (https://wiki.dppeppel.me/doc/grabarr-DLDkKuwwox); the seedbox side (qBittorrent, cleanup) is on **Downloads & seedbox**. Update the Grabarr page in the same task as any change to how grabarr runs (deploy, config, URL, auth, secrets, schedule), and add a line to its `## Change log`.

## Layout
- `cmd/grabarr`: main; also `grabarr healthcheck` (container healthcheck) and `grabarr token create|list|revoke` (API tokens)
- `cmd/seed`: builds `data/preview.db` with demo data for `make preview`
- `internal/config`: YAML with `${ENV}` expansion, loaded strictly (unknown keys fail). `settings.go` is the runtime-settings registry (overrides stored in the DB, editable from the UI)
- `internal/repository`: SQLite (modernc) with embedded golang-migrate migrations in `migrations/NNNN_name.{up,down}.sql`; `0001` is the pre-migrate schema, so never edit it
- `internal/api`: chi routes (`handlers.go`; `TestRouteTable` pins them), JSON envelope `{success, data, error, pagination}`; `web.go` serves the embedded SPA
- `internal/auth`: Pocket ID OIDC, cookie sessions, bearer API tokens (SHA-256 hashes), dev login
- `internal/queue`, `internal/executor` (rsync, extraction), `internal/gatekeeper`, `internal/sync` (the seedbox scanner), `internal/arr` (Radarr/Sonarr), `internal/notifications` (Pushover)
- `web/`: SvelteKit SPA (Svelte 5 runes, plain JS); `web/embed.go` embeds `web/build`
- `deploy/config.yaml`: the production config (in git, no secret values); `config.preview.yaml`: local preview
- `docs/`: API, CONFIGURATION and GATEKEEPER references

## Conventions
- Matches the other Go apps (scryarr, foodtrack): chi, modernc sqlite, golang-migrate, slog, testify. The runtime image is Alpine, not distroless, because grabarr shells out to rsync, ssh, unrar and 7z.
- Schema changes are new migrations; take a `.backup` of production's `grabarr.db` before deploying one.
- Config fields must be used in code the moment they're added; update `deploy/config.yaml`, `config.preview.yaml` and the structs together. Secrets are `${VARS}` from Komodo variables, listed in the `docker-compose.yml` environment (a test checks they match).
- Store tokens only as SHA-256 hashes; secrets never go in URLs or logs.
- Add table-driven tests for new code and run `make test-ci` (format, vet, tests) before committing. Mock external services (mockery mocks in `internal/mocks`).
- SQLite keeps `synchronous=NORMAL` and `busy_timeout` per connection (`buildDSN`); FULL made scans slow enough to time out progress writes on millions.

## UI changes: get approval before building and deploying
UI follows the house-style skill (~/.claude/skills/house-style); web/src/lib/house.css is a vendored copy of its tokens.css: don't edit it here, change the canonical file and re-copy. Use tokens by meaning and never hardcode colors in components.
1. **Mock first.** Before writing real code for a visible change, show a phone-sized mockup (and desktop: David uses grabarr a lot on desktop) and wait for a yes. Skip this only for small tweaks.
2. **Preview locally.** `make preview` (Go API plus built UI on :8080, seeded demo data, dev login) and `make dev-web` (hot reload on :5173); the `preview` and `preview-web` launch configs start them in the browser pane. Check 375px and desktop, light and dark. The Go server has no hot reload: restart `preview` after Go changes.
3. **Deploy only after he approves.**

Extend `cmd/seed` when a feature needs data to look realistic.

## Deploy
- Merge to `main`: Komodo rebuilds the image on millions and restarts the container within about 5 minutes. Don't build or copy images by hand, and don't touch the container in the Unraid Docker UI.
- millions is x86_64; the Dockerfile builds the UI in a Node stage and a static (CGO-off) Go binary.
- The stack entry and secrets are in `~/projects/nas` (`komodo/resources/stacks.toml`) and Komodo variables. The SSH key stays in `/mnt/apps/appdata/grabarr/config/` on millions.
- The container sees `/mnt/user` and `/mnt/cache` as `/unraid/user` and `/unraid/cache`; it runs as 99:100 on the `media` network with no host ports.
- Check a deploy with `docker logs grabarr` on millions and the API with the token in `~/.config/grabarr/token`.

## Scripts
- `scripts/retry-failed-jobs.sh`, `scripts/create-orphaned-jobs.sh`: API maintenance; need `GRABARR_TOKEN`
- `scripts/sonarr-cleanup-downloads.sh`: Sonarr's post-import cleanup of the downloads folder (`test-sonarr-cleanup.sh` tests it)

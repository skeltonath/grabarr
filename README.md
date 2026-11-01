# Grabarr

Copies finished downloads from a seedbox to the NAS. Grabarr scans watched folders on the seedbox over SSH every few minutes, queues a job for each new file, copies it with rsync under a bandwidth and disk-space gatekeeper, extracts archives, tells Radarr and Sonarr when a file has landed, and sends a Pushover alert when a copy fails.

It runs on millions as a Komodo stack, built from this repo on every push to `main`, at https://grabarr.internal.dppeppel.me (LAN and Tailscale, Pocket ID sign-in). How it runs, its secrets and how to operate it are on the wiki: **Media › Acquisition › Downloads & seedbox**.

## Stack

- Go 1.27: chi, modernc SQLite (no CGO), golang-migrate, slog, testify
- Web UI: SvelteKit (Svelte 5) in `web/`, built to `web/build` and embedded in the binary; follows the house style
- Runtime image: Alpine with rsync, ssh, unrar and 7z (grabarr shells out to them)

## Working on it

```bash
make test-ci     # format, vet and all tests: run before committing
make preview     # seeded demo data at http://localhost:8080, nothing touches the seedbox
make dev-web     # UI hot reload on :5173 against `make preview`
make help        # everything else
```

- Config: [`deploy/config.yaml`](deploy/config.yaml) is the production file; see [docs/CONFIGURATION.md](docs/CONFIGURATION.md).
- API: [docs/API.md](docs/API.md). Scripts authenticate with a bearer token from `docker exec grabarr /app/grabarr token create NAME`.
- Gatekeeper: [docs/GATEKEEPER.md](docs/GATEKEEPER.md).
- Deploying: merge to `main`. Komodo rebuilds and restarts the container within about 5 minutes; database migrations run on start-up.

## Scripts

- `scripts/retry-failed-jobs.sh`, `scripts/create-orphaned-jobs.sh`: maintenance against the API (need `GRABARR_TOKEN`).
- `scripts/sonarr-cleanup-downloads.sh`: Sonarr's post-import cleanup of the downloads folder (`scripts/test-sonarr-cleanup.sh` tests it).

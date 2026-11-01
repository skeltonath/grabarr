# Configuration

Grabarr reads one YAML file. In production that's [`deploy/config.yaml`](../deploy/config.yaml), kept in git and mounted read-only at `/etc/grabarr/config.yaml` (`GRABARR_CONFIG` points there). Changing it is a commit to `main`; Komodo redeploys. Locally, `make preview` uses [`config.preview.yaml`](../config.preview.yaml).

The file is the source of truth for everything except the handful of [runtime settings](#runtime-settings) that can be overridden from the web UI.

## Loading

- **Where:** `$GRABARR_CONFIG`, else `/config/config.yaml`, else `./config.yaml`.
- **Secrets:** `${NAME}` anywhere in the file is replaced from the environment when it's loaded. Production secrets are Komodo variables, passed in by the explicit `environment:` list in `docker-compose.yml`. A test (`internal/config/deploy_config_test.go`) fails if the config references a variable compose doesn't pass. `${NAME}` is expanded inside comments too, so don't write one in a comment.
- **Strict:** an unknown or misspelled key stops startup.
- **Validated:** a valid port, `jobs.max_concurrent` > 0, `jobs.max_retries` ≥ 0, Pushover credentials when Pushover is enabled, and sign-in configured (see [auth](#auth)).
- **Durations** are Go durations: `30s`, `5m`, `168h`.

## Sections

### server

| Key | Meaning |
|---|---|
| `port`, `host` | Where the HTTP server listens (`0.0.0.0:8080` in the container). |
| `shutdown_timeout` | How long a graceful shutdown waits. |
| `base_url` | The public origin, `https://grabarr.internal.dppeppel.me`. The Pocket ID redirect URI is `<base_url>/auth/callback`; an `https` origin makes cookies Secure. |

### auth

Every page and API call needs a Pocket ID session or an API token; only `/api/v1/health`, the UI's build files, manifest and icons are open. Grabarr refuses to start unless `oidc` or `dev_login` is set.

| Key | Meaning |
|---|---|
| `oidc.issuer` | `https://id.dppeppel.me` |
| `oidc.client_id`, `oidc.client_secret` | The Pocket ID client `grabarr` (Komodo variables `GRABARR_OIDC_CLIENT_ID` / `_SECRET`). |
| `dev_login` | Signs anyone in as a named user without a password. Local preview only; a test keeps it off in `deploy/config.yaml`. |
| `session_days` | How long a browser session lasts (default 90). |

API tokens for scripts aren't configured here: `docker exec grabarr /app/grabarr token create NAME`.

### remotes

One entry per seedbox. Grabarr only talks to it over SSH (scans with `find`, copies with rsync).

| Key | Meaning |
|---|---|
| `name` | Shown in the UI under each source. |
| `ssh_host`, `ssh_user` | Where to connect. |
| `ssh_key_file` | Private key inside the container (`/config/grabarr_rsa`, from the host's config folder; not in git). |
| `watched_paths` | The folders scanned for new files (below). |

Each watched path:

| Key | Meaning |
|---|---|
| `label` | The source's name in the web UI ("Video", "Books"). Without one the UI shows the remote's name. |
| `remote_path` | Folder on the seedbox, with a trailing slash. |
| `local_path` | Where its files land locally; empty means `downloads.local_path`. The folder structure under `remote_path` is kept. |
| `extensions` | File types to copy. |
| `archive_extensions` | Archives to copy and extract (`rar` also matches `.r00`… parts). |
| `exclude_patterns` | Regexes on the file name to skip, such as samples. |
| `auto_download` | Queue new files automatically; otherwise they wait in Files for a Queue click. |
| `recursive` | Look in subfolders. |

### downloads

| Key | Meaning |
|---|---|
| `local_path` | Default destination (`/unraid/user/media/downloads/` in the container is `/mnt/user/media/downloads/` on millions). |
| `allowed_categories` | Categories accepted when a job is created through the API. |

### gatekeeper

Decides whether a job may start.

| Key | Meaning |
|---|---|
| `seedbox.bandwidth_limit_mbps` | Total transfer ceiling, split across the running jobs; 0 is unlimited. |
| `cache_disk.path`, `cache_disk.max_usage_percent` | New jobs wait while that disk is fuller than this. The UI shows an amber notice meanwhile. |
| `*.check_interval` | How often each is checked. |
| `rules.require_filesize_check` | Check a file fits before starting it (needs the job's size). |

### jobs

| Key | Meaning |
|---|---|
| `max_concurrent` | Copies running at once. |
| `max_retries` | Automatic retries for a failed copy. |
| `cleanup_completed_after`, `cleanup_failed_after` | How long finished jobs stay in the list (an hourly cleanup removes older ones). |

### sync

| Key | Meaning |
|---|---|
| `enabled` | Scan the seedbox periodically. The scan is the only thing that creates jobs; nothing on the seedbox calls grabarr. |
| `scan_interval` | How often. |

### extraction

| Key | Meaning |
|---|---|
| `enabled` | Extract archives once all their parts have copied. |
| `cleanup_archives` | Delete the archive files after a successful extraction. |

### media_managers

Radarr and Sonarr are told when a copy lands, so they import it then rather than retrying against a path that doesn't exist yet.

| Key | Meaning |
|---|---|
| `debounce` | Wait this long for a burst of completions (a season pack) before one refresh. |
| `instances[]` | `name`, `enabled`, `url` (`http://radarr:7878` on the `media` network), `api_key`, and the `categories` it handles. |

### notifications.pushover

`enabled`, `token`, `user`, `priority` (−2 to 2), and `retry_interval` / `expire_time` for priority-2 alerts. Sent on failures and on start-up.

### database, logging, preview

- `database.path`: the SQLite file (`/data/grabarr.db`). The schema is managed by golang-migrate (`internal/repository/migrations/`) and migrated on start-up.
- `logging.level` (`debug`, `info`, `warn`, `error`) and `logging.format` (`json` or `text`).
- `preview: true` serves the UI and API from the database without starting the scheduler, executor, scanner or notifications. Only `config.preview.yaml` sets it; a test keeps it off in `deploy/config.yaml`.

## Runtime settings

A few values can be changed while grabarr runs, from the **Settings** page or `PATCH /api/v1/settings` ([API](API.md#runtime-settings)). A change is stored in the database and wins over the file until it's reset, so an unrelated edit to `deploy/config.yaml` doesn't undo it. Settings shows "Changed here · the file says X · Reset" for each one.

| Setting | Takes effect |
|---|---|
| `jobs.max_concurrent` | Immediately |
| `gatekeeper.seedbox.bandwidth_limit_mbps` | For jobs started afterwards |
| `gatekeeper.cache_disk.max_usage_percent` | Immediately |
| `jobs.max_retries` | For new jobs |
| `jobs.cleanup_completed_after`, `jobs.cleanup_failed_after` | At the next hourly cleanup |
| `sync.enabled`, `sync.scan_interval` | At the next scan tick |

Everything else (paths, remotes, credentials, media managers) is file-only.

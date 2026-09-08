# Implementation Plan: Self-Hosted Fitness Backend

## Goal and fixed decisions

Build a self-hosted Go 1.27 service named `fitnessd`. It becomes the canonical store for Garmin data, manual workouts, and app documents. It exposes REST APIs and an authenticated MCP Streamable HTTP endpoint for ChatGPT Web.

The deployment consists of Caddy, Pocket ID v2, and Fitnessd under Docker Compose:

* `https://auth.aries.timdumol.com` serves Pocket ID.
* `https://api.aries.timdumol.com/mcp` serves MCP.
* `https://api.aries.timdumol.com/api/v1/...` serves REST.
* `https://mars.timdumol.com` continues proxying the existing host service on port 8080.
* Pocket ID and Fitnessd each use a separate SQLite database.
* There is one household and exactly two athletes: `tim` and `jopi`.
* Both approved users may read and write either athlete's data.
* Garmin payloads are stored raw, including GPS coordinates and detailed metrics.
* Keep the simple millisecond timestamp sync cursor. This is acceptable while there is only one mobile device per athlete and workout concurrency remains negligible.

## Execution rules

Follow the numbered steps in order. Do not begin the next step until every check for the current step passes.

* Never read, print, commit, or log Garmin tokens, Pocket ID encryption keys, ingest tokens, OAuth tokens, or database credentials.
* Use committed fixtures with synthetic data in tests.
* Run `go test ./...` after every Go step and the existing Python tests after every Python change.
* Pin all Go modules. Before production, pin container images to tested exact versions or digests rather than floating tags.
* If a check fails, fix it before continuing. Do not weaken a test or skip a security validation merely to advance the plan.

## Target project layout

```text
api/openapi.yaml
cmd/fitnessd/main.go
internal/auth/
internal/config/
internal/db/
internal/httpapi/
internal/ingest/
internal/mcpserver/
internal/sync/
migrations/
testdata/
Dockerfile
docker-compose.yml
Caddyfile
```

Use `net/http` and `log/slog`, [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), [Goose](https://github.com/pressly/goose), [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen), [keyfunc v3](https://github.com/MicahParks/keyfunc), [google/uuid](https://github.com/google/uuid), and the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## Step 1 — Establish and record the baseline

1. Run the existing Python tests without changing application behavior.
2. Ensure `.gitignore` covers `.private/`, `secrets/`, generated databases, database WAL/SHM files, and archive storage.

**Check before continuing**

```bash
env UV_CACHE_DIR=/tmp/fitsync-uv-cache uv run python -m unittest -v
git check-ignore secrets/
git status --short
```

Expected: all existing Python tests pass; each sensitive path is ignored; no sensitive file is staged.

## Step 2 — Scaffold a buildable Go service

1. Create `go.mod` targeting Go 1.27 and add only the pinned dependencies needed initially.
2. Create `cmd/fitnessd/main.go` with subcommands prepared for:
   * `serve`
   * `migrate`
   * `identities list-pending`
   * `identities approve`
   * `identities disable`
   * `healthcheck`
3. Add configuration parsing in `internal/config`. Configuration must support `*_FILE` secrets and must never log secret values.
4. Add an HTTP server with explicit read-header, read-body, write, and idle timeouts, graceful shutdown, structured logs, request IDs, and `GET /healthz`.
5. Add a multi-stage Dockerfile that builds a static application binary and runs it as a non-root user. Include CA certificates; do not put secrets in the image. Add `.dockerignore` entries for `.git`, `.private`, `data/`, `secrets/`, SQLite files, and Python caches.

**Check before continuing**

```bash
go mod tidy
go test ./...
go vet ./...
docker build -t fitsync-fitnessd:plan-check .
docker image inspect fitsync-fitnessd:plan-check
```

Expected: build and tests pass; `fitnessd serve` starts; `/healthz` returns 200; the final container has no source tree or embedded secret files and does not run as UID 0.

## Step 3 — Implement SQLite migrations and repositories

1. Open SQLite with this base DSN:

   ```text
   file:/data/fitness.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)
   ```

2. Use a bounded connection pool. Ensure the pragmas apply to every connection. Run Goose migrations before serving traffic.
3. Create these identity tables:
   * `households`: one seeded household.
   * `athletes`: stable text/UUID IDs, unique handle, `household_id`, and `active`. Seed inactive `tim` and `jopi` rows.
   * `athlete_identities`: `athlete_id`, exact `issuer`, exact `subject`, display-only issuer-provided username, approval timestamps, and `UNIQUE(issuer, subject)`. Do not authorize by username.
   * `pending_identities`: random ID, issuer, subject, issuer-provided username, first/last seen timestamps, expiry, and `UNIQUE(issuer, subject)`.
   * `identity_audit_log`: append-only approval, disable, and rebind events.
4. Create `garmin_records` with:
   * `athlete_id`, `dataset`, `record_key`.
   * athlete-local `record_date` as `YYYY-MM-DD`.
   * nullable `occurred_at_utc_ms` as Unix milliseconds.
   * nullable `tz_offset_seconds` with a `-64800..64800` check.
   * `payload_format`, `payload_raw`, `payload_sha256`, and `ingested_at`.
   * `CHECK(json_valid(payload_raw))`.
   * `UNIQUE(athlete_id, dataset, record_key)` and an athlete foreign key.
5. Add generated columns for demonstrated queries. Sleep score must support both formats during migration:

   ```sql
   COALESCE(
     json_extract(payload_raw, '$.sleep.dailySleepDTO.sleepScores.overall.value'),
     json_extract(payload_raw, '$.sleep.dailySleepDTO.sleepScore'),
     json_extract(payload_raw, '$.sleep_score')
   )
   ```

   Add fixture tests for every JSON path before indexing it.
6. Create `garmin_archives` with athlete/activity association, safe generated path, original filename for display only, size, SHA-256, content type, creation time, and expiry time.
7. Create `workout_sessions`, `workout_entries`, and `app_documents`. Use text IDs with UUIDv7 required for all new syncable rows. Include `athlete_id`, `client_updated_at`, server-generated `server_synced_at`, and `deleted_at`. Add foreign keys and ordering constraints.
8. Store all application timestamps as Unix milliseconds. Do not pretend SQLite text timestamps are timezone-aware.
9. Keep ingest-token mappings in configuration memory: at startup read each per-athlete token file, hash it, retain only the hash, and discard the plaintext bytes. No ingest token or hash belongs in logs.

**Check before continuing**

```bash
go test ./...
go run ./cmd/fitnessd migrate --db /tmp/fitsync-plan.db
go run ./cmd/fitnessd migrate --db /tmp/fitsync-plan.db
sqlite3 /tmp/fitsync-plan.db 'PRAGMA foreign_key_check; PRAGMA integrity_check; PRAGMA journal_mode;'
```

Expected: migrations are idempotent; foreign-key check emits no rows; integrity is `ok`; journal mode is `wal`; tests prove invalid JSON, invalid offsets, duplicate identities, and cross-table orphan rows are rejected.

## Step 4 — Define OpenAPI and implement REST/LWW sync

1. Write `api/openapi.yaml` before handlers. Generate Go request/response types and server interfaces into `internal/httpapi/generated.go` with `oapi-codegen`; do not hand-maintain duplicate transport structs.
2. Define bounded, paginated read endpoints:
   * `GET /api/v1/athletes/{id}/health/daily?from=YYYY-MM-DD&to=YYYY-MM-DD&limit=&cursor=`
   * `GET /api/v1/athletes/{id}/activities?from=YYYY-MM-DD&to=YYYY-MM-DD&limit=&cursor=`
3. Define:
   * `GET /api/v1/sync/pull?since=<unix_millis>`
   * `POST /api/v1/sync/push`
4. Pull returns rows where `server_synced_at > since`, including tombstones. Return a server timestamp obtained after the read transaction completes as the next high-water mark.
5. Push accepts client-generated UUIDv7 IDs for new records. The server assigns `server_synced_at`; clients cannot supply or override it.
6. Resolve conflicts with `client_updated_at`. A strictly newer client mutation wins; equal or older mutations do not overwrite the stored row. Return the stored winner for every submitted record so the client can converge.
7. Apply a session and its entry mutations in one database transaction. Define deletion tombstones explicitly and never hard-delete syncable rows during normal operation.
8. Document the accepted limitation: this timestamp cursor assumes one mobile device per athlete and negligible concurrent workout writes. Garmin ingestion uses separate tables. Replace it with a sequence cursor if those assumptions change.
9. Enforce request-body, batch-size, range, and page-size limits in both the OpenAPI contract and handlers.
10. Keep authorization behind an injected interface and use a fake household principal in tests. Do not expose these handlers outside localhost/test servers until the real authorization middleware in Step 6 is complete.

**Check before continuing**

```bash
go generate ./...
sha256sum internal/httpapi/generated.go > /tmp/fitsync-openapi.sha256
go generate ./...
sha256sum --check /tmp/fitsync-openapi.sha256
go test ./...
```

Expected: generated code is current; contract tests cover create/update/equal-time/stale/delete/pull behavior, transaction rollback, pagination boundaries, invalid UUIDs, and oversized batches.

## Step 5 — Implement raw Garmin ingestion and archive retention

1. Replace the single shared ingest token with one random token per athlete. At startup, hash each configured token and compare request credentials in constant time; retain only hashes in memory. Map each credential to exactly one athlete. Configure token files as `FITNESS_INGEST_TIM_TOKEN_FILE` and `FITNESS_INGEST_JOPI_TOKEN_FILE`; do not accept an athlete ID from an untrusted body as authority.
2. Implement `POST /api/v1/ingest/garmin/batch` as `multipart/form-data`:
   * One `records` part containing bounded JSON metadata and `payload_raw` values.
   * Zero or more archive parts referenced by generated upload IDs, never by filesystem paths.
3. For sleep, use a stable raw envelope:

   ```json
   {"sleep": {}, "hrv": {}, "resting_heart_rate": {}}
   ```

   Each object contains the corresponding original Garmin response without redaction. Activity details retain coordinates, polylines, and metric geometry.
4. Encode each `payload_raw` deterministically in Python with sorted keys and compact separators. Decode it into `json.RawMessage` in Go, validate it, and have the server calculate SHA-256 from the received raw JSON bytes; do not trust a client-supplied hash. A retried Python batch must produce identical payload bytes. On `(athlete_id, dataset, record_key)` conflict, update payload, format, hash, parsed date/time metadata, and `ingested_at` only when the server-calculated hash changes.
5. Write archives to a temporary file, enforce total/request/file size limits, calculate the hash while streaming, `fsync`, and atomically rename to a server-generated path. Never use an uploaded filename as a path.
6. Insert archive metadata only after the final file exists. On database failure, remove the new file. On startup and daily, reconcile orphan files and metadata, then remove expired archives older than 90 days.
7. Update `garmin_sync.py` to upload raw payloads while retaining the existing curated JSON/CSV outputs for local human use. Stop removing location fields from the ingestion payload only; existing curated exports may remain GPS-stripped if desired.
8. Add retry with bounded exponential backoff. Re-sending the same batch must be idempotent.

**Check before continuing**

```bash
go test ./...
env UV_CACHE_DIR=/tmp/fitsync-uv-cache uv run python -m unittest -v
```

Expected: synthetic integration tests prove token-to-athlete binding, raw GPS preservation, no-op duplicate uploads, Garmin revision replacement, path-traversal rejection, size enforcement, atomic rollback, startup cleanup, and 90-day expiry. Existing Python tests still pass.

## Step 6 — Implement Pocket ID identity binding and API authorization

1. At startup, fetch Pocket ID discovery from the configured exact issuer. Use the advertised `jwks_uri`; do not hard-code a JWKS path. Unit/integration tests at this stage must use a local `httptest` OIDC issuer; they must not depend on the production Pocket ID deployment from Step 9.
2. Cache JWKS with `keyfunc`, refresh periodically, and refresh when an unknown `kid` appears.
3. Accept OAuth access tokens only. Validate:
   * allowed signing algorithm;
   * signature;
   * exact issuer;
   * expiry and not-before;
   * audience/resource `https://api.aries.timdumol.com/mcp`;
   * required `fitness:read` or `fitness:write` scope.
4. Resolve the exact `(issuer, sub)` through `athlete_identities`. Never use username as the authorization key.
5. For a valid but unknown identity, upsert a short-lived pending record containing the issuer-provided username and return `403 identity_pending`. When the access token lacks user claims, obtain them from the issuer's advertised UserInfo endpoint and require its `sub` to match the already validated token subject. Do not return athlete data or reveal whether a requested athlete exists. Already-bound identities use their stored display username and do not require `profile` claims or UserInfo.
6. Implement local administrative CLI commands:
   * `identities list-pending`
   * `identities bind --subject <id> --username <username> --athlete tim|jopi`
   * `identities approve --pending-id <id> --athlete tim|jopi`
   * `identities disable --athlete tim|jopi`
7. Binding and approval must verify the Pocket ID username is exactly the selected athlete handle, activate the athlete, create the immutable binding, append an audit record, and refuse to replace an existing binding. The normal provisioning path obtains subjects through the Pocket ID API-key sync script and uses `identities bind`; pending approval remains only as a defensive fallback.
8. After approval, either athlete may target either household athlete. Every query and mutation must still constrain the target by the caller's `household_id`.
9. Add bounded rate-limit buckets with eviction. Use the authenticated subject when available; use Caddy-provided IP only from a trusted proxy.

**Check before continuing**

```bash
go test ./...
```

Expected: tests reject wrong issuer, audience, algorithm, scope, expired token, future token, ID token, unknown identity, inactive identity, and athlete outside the household. Tests prove `tim` and `jopi` can each access both household athletes only after explicit approval.

## Step 7 — Implement MCP and OAuth protected-resource behavior

1. Pin a tested official MCP Go SDK version.
2. Serve protected resource metadata at `/.well-known/oauth-protected-resource`. It must identify:
   * resource: `https://api.aries.timdumol.com/mcp`;
   * authorization server: `https://auth.aries.timdumol.com`;
   * supported scopes: `fitness:read` and `fitness:write`.
3. Unauthorized MCP HTTP requests return `401`, not an MCP tool error, with a `WWW-Authenticate: Bearer` challenge containing `resource_metadata` and the needed scope.
4. Use Pocket ID's advertised OAuth/OIDC endpoints. Prefer OAuth Client ID Metadata Documents for public MCP clients; use DCR only if both Pocket ID and the connecting client advertise it. If neither is advertised, create a manual confidential client and configure its exact ChatGPT callback URL. Ensure authorization-code + PKCE and refresh tokens/offline access are enabled.
5. Create `mcp.NewStreamableHTTPHandler` with `StreamableHTTPOptions.Stateless=true`, request-cancellation propagation enabled, and a finite request-body limit. Let the SDK own its channels and session lifecycle.
6. Initially expose only:
   * `get_daily_health`: read-only; optional `target_athlete_id`; bounded date range.
   * `get_activities`: read-only; optional target; bounded date range and result count.
   * `update_workout`: write operation; optional target; uses the same transactional LWW service as REST.
7. If target is omitted, use the athlete resolved from `(issuer, sub)`. If supplied, require the same household.
8. Give every tool a strict JSON schema, concise bounded output, and correct read-only/destructive/idempotent annotations. Do not return raw payload collections unless the tool explicitly requests a small bounded subset.

**Check before continuing**

```bash
go test ./...
```

Expected: protocol tests pass for initialization/tool discovery/tool calls, unauthorized requests produce the required HTTP challenge, read/write scopes are separated, cancellation stops a synthetic long-running handler, tool outputs remain bounded, and cross-household fixture access fails.

## Step 8 — Create the production Compose and Caddy configuration

The repository-level Compose file in this step is also a self-contained
integration/standalone configuration. On `aries.timdumol.com`, Step 9 renders
the production variant without its `caddy` service and installs the site blocks
as a fragment in the host's existing Caddy deployment. The backend service
definitions, secrets, health checks, and routing behavior remain the same.

1. Create secret files outside Git:
   * `secrets/pocketid_encryption_key`
   * `secrets/fitness_ingest_tim_token`
   * `secrets/fitness_ingest_jopi_token`
2. Use this network shape. Only Caddy publishes host ports; Pocket ID and Fitnessd use Compose service discovery:

   ```yaml
   services:
     caddy:
       image: caddy:2 # pin exact version/digest before production
       restart: unless-stopped
       ports:
         - "80:80"
         - "443:443"
       volumes:
         - ./Caddyfile:/etc/caddy/Caddyfile:ro
         - caddy_data:/data
         - caddy_config:/config
       extra_hosts:
         - "host.docker.internal:host-gateway"
       depends_on:
         pocketid:
           condition: service_healthy
         fitnessd:
           condition: service_healthy

     pocketid:
       image: ghcr.io/pocket-id/pocket-id:v2 # pin exact version/digest
       restart: unless-stopped
       expose:
         - "1411"
       volumes:
         - ./data/pocketid:/app/data
       environment:
         APP_URL: https://auth.aries.timdumol.com
         ENCRYPTION_KEY_FILE: /run/secrets/pocketid_encryption_key
         ALLOW_INSECURE_CALLBACK_URLS: "false"
         TRUST_PROXY: "true"
       secrets:
         - pocketid_encryption_key
       healthcheck:
         test: ["CMD", "/app/pocket-id", "healthcheck"]
         interval: 30s
         timeout: 5s
         retries: 5
         start_period: 15s

     fitnessd:
       build: .
       restart: unless-stopped
       expose:
         - "3000"
       volumes:
         - ./data/fitnessd:/data
         - ./data/archives:/var/lib/fitnessd/archives
       environment:
         DB_PATH: /data/fitness.db
         ARCHIVE_DIR: /var/lib/fitnessd/archives
         POCKETID_ISSUER: https://auth.aries.timdumol.com
         MCP_RESOURCE: https://api.aries.timdumol.com/mcp
         FITNESS_INGEST_TIM_TOKEN_FILE: /run/secrets/fitness_ingest_tim_token
         FITNESS_INGEST_JOPI_TOKEN_FILE: /run/secrets/fitness_ingest_jopi_token
       secrets:
         - fitness_ingest_tim_token
         - fitness_ingest_jopi_token
       healthcheck:
         test: ["CMD", "fitnessd", "healthcheck"]
         interval: 30s
         timeout: 5s
         retries: 5
         start_period: 10s

   secrets:
     pocketid_encryption_key:
       file: ./secrets/pocketid_encryption_key
     fitness_ingest_tim_token:
       file: ./secrets/fitness_ingest_tim_token
     fitness_ingest_jopi_token:
       file: ./secrets/fitness_ingest_jopi_token

   volumes:
     caddy_data:
     caddy_config:
   ```

3. Use this routing shape:

   ```caddyfile
   auth.aries.timdumol.com {
       reverse_proxy pocketid:1411
   }

   api.aries.timdumol.com {
       handle /.well-known/oauth-protected-resource* {
           reverse_proxy fitnessd:3000
       }
       handle /mcp {
           reverse_proxy fitnessd:3000 {
               flush_interval -1
           }
       }
       handle /api/* {
           reverse_proxy fitnessd:3000
       }
       handle /healthz {
           reverse_proxy fitnessd:3000
       }
       respond 404
   }

   mars.timdumol.com {
       reverse_proxy host.docker.internal:8080
   }
   ```

4. Pocket ID and Fitnessd must have no `ports:` entries. `TRUST_PROXY=true` is acceptable only while Pocket ID has no host-published port and only controlled Compose peers can reach it.
5. Configure bounded Docker log rotation. Mount data directories with ownership matching the non-root container users.

**Check before continuing**

```bash
docker compose config
docker compose build
docker compose up -d
docker compose ps
curl --fail https://api.aries.timdumol.com/healthz
curl --fail https://auth.aries.timdumol.com/.well-known/openid-configuration
curl --fail https://api.aries.timdumol.com/.well-known/oauth-protected-resource
```

Expected: Compose validates; all health checks pass; TLS is valid; discovery documents contain the configured public HTTPS URLs; host ports 1411 and 3000 are not listening publicly; an unauthenticated `/mcp` request returns the OAuth `401` challenge.

## Step 9 — Provision and deploy with Ansible

1. Add a `deploy/ansible/` playbook and inventory for `aries.timdumol.com`. The
   playbook must connect as the passwordless-SSH deployment user and use
   `become` only for package installation, `/opt` deployment files, Docker, and
   service management. Do not require the SSH user to be a member of the
   `docker` group.
2. Treat the Caddy container already managed by the `aprendiendo` deployment at
   `/opt/aprendiendo-mcp/app` as the single host edge proxy. Fitnessd's
   production Compose project must not publish ports 80/443 and must not start
   a second Caddy container.
3. Coordinate the `aprendiendo` Ansible configuration with this deployment:
   * create one external bridge network named `caddy-edge`;
   * attach the existing Caddy service and the Fitnessd and Pocket ID services
     to that network, retaining each project's private default network;
   * give the backends unambiguous aliases such as `fitsync-fitnessd` and
     `fitsync-pocketid` on `caddy-edge`;
   * mount a root-owned `/opt/caddy/sites-enabled` directory read-only at
     `/etc/caddy/sites-enabled` in the Caddy container;
   * make the Caddyfile generated by the `aprendiendo` playbook contain
     `import sites-enabled/*` alongside its own `mars.timdumol.com` site.

   The corresponding change must be committed in `~/devel/aprendiendo`; do not
   patch only the generated file on the server, because a later Aprendiendo
   release would remove the import and shared-network attachment.
4. Have the Fitnessd playbook install only its owned site fragment at
   `/opt/caddy/sites-enabled/fitsync.caddy`. Route `auth.aries.timdumol.com` to
   `fitsync-pocketid:1411` and `api.aries.timdumol.com` to
   `fitsync-fitnessd:3000`. Do not redefine or overwrite
   `mars.timdumol.com`. Caddy supports a glob include through the Caddyfile
   `import` directive; an empty glob is valid.
5. Copy a deliberate allowlist of source/build/deployment files into
   `/opt/fitsync/app`; do not synchronize the whole working tree. In
   particular, never upload `.git`, `.private`, local
   databases, Garmin data, Python caches, editor settings, or plaintext
   secrets.
6. Create persistent directories for Fitnessd, Pocket ID, and archives under
   `/opt/fitsync`. Set ownership to the numeric
   non-root users used by the pinned images. The playbook must preserve
   existing databases, keys, archives, and imported data on every rerun.
7. Generate the Pocket ID encryption key and both athlete ingest tokens on the
   server only when absent. Use cryptographically secure random generation,
   write without a trailing newline where required, set mode `0400`, mark all
   secret-handling tasks `no_log: true`, and never copy secret values back to
   the controller. Support an operator-supplied Ansible Vault value for each
   secret without requiring secrets in Git.
8. Render the production Compose file with pinned image versions/digests,
   bounded log rotation, health checks, no published backend ports, and the
   external `caddy-edge` network. Run `docker compose config --quiet` before
   changing running containers.
9. Deploy in this order: ensure Docker/Compose, create the external network and
   directories, render and validate the Compose file, promote the Fitnessd Caddy
   site fragment, validate the full Caddy configuration inside the existing Caddy
   container, and reload Caddy. Start Pocket ID first, wait for its container
   health check and the public OIDC discovery URL, then start Fitnessd and wait
   for its health check. Revalidate and reload the combined Caddy configuration
   before the final public checks. This ordering matters because Fitnessd fetches
   OIDC discovery during startup. Never reload an invalid combined configuration.
10. Make failure recoverable: keep the previous Fitnessd site fragment and
    Compose file until validation succeeds; if Caddy validation or public
    health checks fail, restore the previous fragment and reload the last valid
    combined configuration. An initial failed deployment must remove its new
    fragment so `mars.timdumol.com` remains served.
11. Keep interactive Pocket ID setup, user/passkey enrollment, subject sync,
    out of the unattended playbook. Print the exact non-secret operator commands
    needed for Step 10 after a successful
    deployment, without transferring or printing the API key.
12. Add a repository entrypoint such as `scripts/deploy.sh` that runs
    `ansible-playbook` with the committed inventory, supports `--check`, and
    refuses to deploy from a failed `./scripts/test.sh` or
    `./scripts/build.sh`. Document required controller packages and the
    coordinated Aprendiendo deployment prerequisite.

**Check before continuing**

```bash
ansible-playbook --syntax-check -i deploy/ansible/inventory/hosts.yml deploy/ansible/site.yml
ansible-playbook --check --diff -i deploy/ansible/inventory/hosts.yml deploy/ansible/site.yml
./scripts/deploy.sh
ssh aries.timdumol.com 'sudo docker compose -f /opt/fitsync/app/compose.production.yaml ps'
curl --fail https://mars.timdumol.com/health
curl --fail https://api.aries.timdumol.com/healthz
curl --fail https://auth.aries.timdumol.com/.well-known/openid-configuration
curl --fail https://api.aries.timdumol.com/.well-known/oauth-protected-resource
```

Expected: a second playbook run is idempotent; `mars.timdumol.com` remains
healthy throughout; exactly one Caddy container publishes 80/443; Fitnessd and
Pocket ID have no host-published ports; all three public hosts have valid TLS;
and no secret value appears in Ansible, Docker Compose, or service logs.

## Step 10 — Provision Pocket ID and bind `tim` and `jopi`

1. Complete Pocket ID's initial setup at the HTTPS URL.
2. Create exactly two users: `tim` and `jopi`. Make `tim` the administrator rather than creating a shared third admin account.
3. Each person enrolls their own passkey and securely stores their own recovery material. Do not share passkeys or recovery codes.
4. Register the Fitness MCP API/resource as `https://api.aries.timdumol.com/mcp` with `fitness:read` and `fitness:write` permissions, and grant those permissions to the ChatGPT OAuth client as user-delegated access. The deployed Pocket ID `v2.12.0` advertises neither DCR nor client ID metadata documents, so use a manually created confidential client with its exact ChatGPT callback URL; leave ChatGPT's Registration URL empty. Enable authorization-code + PKCE and refresh tokens/offline access. The OAuth access token must contain `sub`; `profile` is not required for the pre-provisioned flow.
5. Before either person connects ChatGPT, run `./scripts/sync-pocket-id-identities.sh` from the repository controller. It calls Pocket ID's `/api/users` endpoint directly with the local `.pocket-id-api-key`, requires exactly the `tim` and `jopi` usernames, extracts their UUID subjects, and invokes the remote Fitnessd `identities bind` command. The key is never copied to the server.
6. Verify `fitnessd identities list` shows exactly two active `(issuer, sub)` bindings and `identities list-pending` is empty. The bind operation is transactional and idempotent; rerun the sync script after a failed or interrupted attempt.
7. Connect ChatGPT as `tim` and `jopi`. No pending auth request or approval script is needed. Pending identities remain a diagnostic fallback for an unexpected subject; do not approve one during normal setup without investigating why the sync did not match.
8. Do not manually copy a username into an authorization table and do not treat username as a stable identity. The bound `(issuer, sub)` is authoritative.

**Check before continuing**

```bash
docker compose exec fitnessd fitnessd identities list
docker compose exec fitnessd fitnessd identities list-pending
```

Expected: exactly two active athletes and two distinct approved `(issuer, sub)` bindings exist; no pending identities remain; neither binding can be approved a second time or rebound without an explicit administrative rotation command.

## Step 11 — Run end-to-end acceptance tests

1. Connect ChatGPT Web to `https://api.aries.timdumol.com/mcp`.
2. Test the complete OAuth path: protected-resource discovery, Pocket ID discovery, login, consent, token issuance, token refresh, and reconnect after expiry.
3. As `tim`, list tools, read `tim`, read `jopi`, create/update a synthetic workout for `tim`, and delete it through a tombstone.
4. As `jopi`, repeat reads for both athletes and perform a synthetic write for `jopi`.
5. Confirm write tools are presented as writes and require the expected confirmation behavior.
6. Run the raw Garmin sync for `tim`. Confirm overlapping migrated rows transition from `legacy_curated_v1` to `garmin_raw_v1` without changing record identity or duplicating rows.
7. Disconnect an in-flight synthetic long MCP request and confirm server work is cancelled and goroutine/session counts return to baseline.
8. Verify direct requests to unpublished backend ports fail from outside the Compose network.

**Check before continuing**

```bash
go test ./...
env UV_CACHE_DIR=/tmp/fitsync-uv-cache uv run python -m unittest -v
docker compose logs --since=30m fitnessd pocketid caddy
```

Expected: all automated and manual scenarios pass; logs contain request IDs and useful errors but no tokens, raw payloads, health values, credentials, or secret material; no duplicate Garmin/workout records appear.

## Step 12 — Back up, restore-test, and cut over

1. Create `scripts/backup_fitness.sh` and `scripts/restore_test.sh`. Use SQLite's online `.backup` command for both active databases; never copy only a WAL-mode main database file.
2. Back up:
   * `fitness.db` snapshot.
   * `pocket-id.db` snapshot.
   * Garmin archive directory.
   * Pocket ID encryption key in a separate encrypted password manager or backup vault.
   * Deployment configuration without plaintext secrets.
3. Use `mktemp`-derived snapshot names, `set -euo pipefail`, restrictive `umask`, and a cleanup trap so overlapping backup jobs cannot collide or leave plaintext snapshots on the server.
4. Encrypt off-host backups, define local retention, verify hashes after transfer, and alert on failure.
5. Restore into a temporary isolated Compose project. Confirm both databases pass integrity checks, Pocket ID can decrypt its signing material, both users can authenticate, and Fitnessd row counts match production.
6. Only after a successful restore test, point normal Garmin sync at Fitnessd.

**Final check**

```bash
sqlite3 <restored-fitness.db> 'PRAGMA integrity_check; PRAGMA foreign_key_check;'
sqlite3 <restored-pocket-id.db> 'PRAGMA integrity_check;'
```

Expected: integrity is `ok`, foreign-key check emits no rows, restored authentication and representative REST/MCP queries work, archive hashes match, and a fresh Garmin sync is idempotent. At this point Fitnessd is the canonical store and the implementation is complete.

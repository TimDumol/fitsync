# Fitnessd production deployment

This playbook deploys the Go Fitnessd service and Pocket ID to
`aries.timdumol.com`. It deliberately does not run Caddy: the existing Caddy
container from the Aprendiendo deployment remains the single host edge proxy.
Fitnessd and Pocket ID have no published host ports and join the private
Fitnessd Compose network plus the external `caddy-edge` network. Caddy reaches
them through the stable aliases `fitsync-fitnessd` and `fitsync-pocketid`.

## Prerequisites

1. Deploy the coordinated Aprendiendo change from `~/devel/aprendiendo` first.
   Its Caddy service must mount `/opt/caddy/sites-enabled` read-only, join
   `caddy-edge`, and import `sites-enabled/*`. The Fitnessd playbook checks
   this contract before changing its own files.
2. Install `ansible-core` (or Ansible), an SSH client, `rsync`, and Docker Compose v2 on
   the controller. The target needs Debian/Ubuntu `apt`, Docker, and
   passwordless `sudo` for the SSH deployment account; it must not need the
   Docker group.
3. Copy the inventory and set the actual deployment account:

   ```bash
   cp deploy/ansible/inventory/hosts.example.yml deploy/ansible/inventory/hosts.yml
   ```

4. Optionally copy `group_vars/aries/secrets.example.yml` to
   `group_vars/aries/secrets.yml` and encrypt it with `ansible-vault`. Values
   supplied there are used only to initialize absent files. Existing Pocket ID
   keys and ingest tokens are preserved on reruns.

The playbook generates missing secrets on the server using `openssl rand`,
writes them without a trailing newline with mode `0400`, and marks every
secret-handling task `no_log`. Secret values are never copied back to the
controller or included in Compose.

The deliberate deployment allowlist is `Dockerfile`, `go.mod`, `go.sum`,
`cmd/`, `internal/`, and `migrations/`. The playbook does not upload local
Garmin data, databases, editor files, caches, or plaintext secrets.

## Deploy

From the repository root:

```bash
./scripts/deploy.sh
```

If `group_vars/aries/secrets.yml` exists and is Ansible-Vault encrypted, add
`--ask-vault-pass`. The deploy script skips that prompt when the file is absent
because missing secret values are generated on the server.

The script runs `./scripts/test.sh` and `./scripts/build.sh` first and refuses
to run Ansible if either fails. `./scripts/deploy.sh --check --diff` passes
those flags to Ansible after the same checks.

The playbook validates the candidate Compose file before starting containers,
waits for both backend health checks, validates the complete Caddyfile inside
the existing edge container, reloads it, and checks Mars, Fitnessd, and Pocket
ID over public HTTPS. It then runs `fitnessd mcp-smoke` inside the deployed
Fitnessd container. That check enumerates every registered MCP action, verifies
that every action exposes input and output schemas, and executes every
read-only action against the production SQLite data through the same typed
handlers and MCP output validation used by the server. Write actions are
covered by the local regression suite and are never invoked by the release
smoke. Previous Compose and site-fragment files remain in the
root-owned rollback directory. Any failed validation restores them and reloads
the last valid Caddy configuration; an initial failed deployment removes its
new fragment so `mars.timdumol.com` remains served.

The playbook prints the non-secret operator commands for Pocket ID setup after
a successful deployment. Pocket ID setup, passkey enrollment, and subject
sync intentionally remain interactive and unattended-playbook safe. Run `./scripts/sync-pocket-id-identities.sh` from the repository
controller after creating `.pocket-id-api-key`; the script calls Pocket ID
directly and only sends the resulting bind commands to Fitnessd over SSH.

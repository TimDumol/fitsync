# Public repository security review

Reviewed on 2026-10-12, starting from commit `201a2e4`.

No exposed credentials were identified in the current publishable files,
reachable Git history, or commit messages. The security fixes listed below
are in the working tree and should be committed before publishing.

## Scope and evidence

- Scanned all 13 reachable commits, including 513 unique historical blobs,
  using Foxguard 0.12.0 secret detection. Also checked credential assignments,
  credential-bearing connection URLs, private-key headers, email addresses,
  and historical paths for databases, exports, tokens, and signing keys.
  Reviewed supplemental matches as synthetic tests or code expressions.
- Scanned current tracked and nonignored untracked files, and commit metadata.
  No secret findings. Ignored local credential and data files are outside the
  publication set; this review does not certify those files for sharing.
- Reviewed HTTP/OIDC authorization, household isolation, MCP authorization,
  bounded request parsing, SQL construction, archive paths, Android credential
  storage, backup rules, and deployment configuration. Foxguard's static
  alerts were reviewed: the reported SQL statements use parameter binding or
  fixed identifiers, the alleged template parsing calls are UUID parsing,
  and the reported filesystem/URL sources are fixed or operator controlled.
- Queried current vulnerability databases for Go dependencies, 43 Python/Maven
  package-version entries, and nine Python packages from the Android build.
  Final scans reported no known vulnerabilities for those checked versions.
  The modified Android Garmin fork has the relevant upstream fix backported.

## Fixes made

- Workout deletion now requires `fitness:write`; previously DELETE requests
  were accepted with `fitness:read`. PATCH also requires write scope.
- The API rejects requests when constructed without an authorizer; previously
  it silently installed an authorizer that allowed all requests.
- The OIDC adapter now provides a verified household ID to legacy sync reads.
  Previously `/api/v1/sync/pull` could return records from other households.
  A missing household now causes denial instead of an unfiltered read.
- Added authorization regression tests, including two-household isolation.
- Updated Go to 1.27.2, including the digest-pinned Docker builder, and patched
  kin-openapi, x/sys, x/text, and their required transitive dependencies.
- Updated desktop garminconnect to 0.3.5 and constrained urllib3 and virtualenv
  to patched versions. Pinned Android urllib3 to 2.8.0.
- Backported the Garmin token-store fix to the custom Android vendor without
  replacing its mobile transport checks. New and existing token files use
  owner-only permissions; supported platforms reject symlink writes. Added
  tests under umask 022 for new/existing files and symlink rejection.
- Added Android signing-store exclusions to Git and credential, local config,
  archive, and build-output exclusions to the Docker context.
- Changed CI secret scanning to explicitly scan the current tree and every
  reachable commit, with full-depth checkout. The previous pre-commit hook
  used `--changed` with no filenames, even when invoked with `--all-files`.

Relevant upstream advisories include
[Garmin token permissions](https://github.com/advisories/GHSA-wjhr-76vg-2hvc),
[urllib3 streaming](https://github.com/advisories/GHSA-gh4c-6fx4-qh6g),
[virtualenv activation](https://github.com/advisories/GHSA-p58f-9548-mpm2), and
[Go HTTP header parsing](https://pkg.go.dev/vuln/GO-2026-6608).

## Verification

- `./scripts/test.sh`: Go tests, go vet, and 18 Python tests passed.
- `./scripts/build.sh`: server binary built successfully.
- `./scripts/build-mobile.sh`: Android unit-test task, release lint, and APK
  build passed. Four Android tests were skipped by their existing setup.
- `govulncheck ./...`: no vulnerabilities found after the final upgrades.
- OSV dependency queries: no findings for the checked final versions.
- Executed the new CI tree/history scan commands locally: no secret findings.
- `git diff --check`: passed.

Repository build/test entrypoints were run outside the sandbox as required.

## Publication considerations and limits

The repository and history identify public service domains under
`timdumol.com`, the athlete handles `tim` and `jopi`, deployment paths, and Git
authors. These are not authentication credentials, but publishing the existing
history makes that information public. Removing it from current files alone
would not remove it from history.

This is a source and dependency audit, not a guarantee that every vulnerability
is absent. Container OS packages, Pocket ID/Caddy images, bundled native
libraries, device behavior, live hosting, and OAuth provider settings were not
independently audited. No production requests, credential rotation, deployment,
history rewriting, or public push were performed. Historical application
versions are not certified for deployment. Future commits and dependency
changes need fresh scans.

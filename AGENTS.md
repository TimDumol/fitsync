# Repository instructions

Use the repository entrypoints for routine verification:

- `./scripts/build.sh` builds the local `fitnessd` binary at `/tmp/fitsync-fitnessd`.
- `./scripts/build.sh --docker` builds the container image `fitsync-fitnessd:local`.
- `./scripts/test.sh` runs Go tests, `go vet`, and the Python test suite.
- Use `./scripts/test.sh --go`, `./scripts/test.sh --vet`, or `./scripts/test.sh --python` for focused iteration.

These scripts keep Go and uv caches in `/tmp` and keep build artifacts out of the repository. Run them from any directory; each script resolves the repository root itself.

Keep tool context small when investigating or changing the code:

- Search targeted paths with `rg`; do not recursively inspect parent directories or generated/private data.
- Read only the relevant file ranges, and avoid printing database dumps, credentials, tokens, or health data.
- Prefer the focused test mode while iterating, then run `./scripts/test.sh` and `./scripts/build.sh` before handing off changes.
- Do not add generated databases, Neon dumps, local secrets, or build output to commits.

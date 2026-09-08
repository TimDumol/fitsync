# Repository instructions

Use the repository entrypoints for routine verification. RUN THESE OUT OF SANDBOX, ALWAYS.

- `./scripts/build.sh` builds the local `fitnessd` binary at `/tmp/fitsync-fitnessd`.
- `./scripts/build.sh --docker` builds the container image `fitsync-fitnessd:local`.
- `./scripts/test.sh` runs Go tests, `go vet`, and the Python test suite.
- Use `./scripts/test.sh --go`, `./scripts/test.sh --vet`, or `./scripts/test.sh --python` for focused iteration.
- `./scripts/build-mobile.sh` to build the mobile APK
- `./scripts/test-mobile-e2e.sh`
# embedded/

This directory holds the standalone **pi** distributions that the release
build of laelia embeds via `//go:embed embedded/dist-<goos>-<goarch>` in
`../binary_release_*.go`.

Each `dist-<goos>-<goarch>/pi` file is an **empty placeholder** so the
`release`-tagged Go build compiles on a fresh checkout. It is NOT a runnable
binary. Before building a release, run `scripts/build-pi.sh` with the target
`GOOS/GOARCH` to download the real pi distribution and populate the matching
`dist-<goos>-<goarch>/` directory. The whole distribution (binary + `theme/`,
`node_modules/`, `package.json`, `photon_rs_bg.wasm`, ...) must be embedded
because pi resolves its runtime assets relative to its own executable.
`ResolveBinary` in `binary_release_*.go` rejects an empty embed at runtime
with a clear error.

Do not commit the real (multi-MB) distributions here — `scripts/build-pi.sh`
materializes them per build.

# embedded/

This directory holds the standalone **pi** distribution that the release
build of laelia embeds via `//go:embed embedded/dist` in
`../binary_release.go`.

The committed `dist/pi` file is an **empty placeholder** so the
`release`-tagged Go build compiles on a fresh checkout. It is NOT a runnable
binary. Before building a release, run `scripts/build-pi.sh` to download the
real pi distribution for the target `GOOS/GOARCH` and populate `dist/`. The
whole distribution (binary + `theme/`, `node_modules/`, `package.json`,
`photon_rs_bg.wasm`, ...) must be embedded because pi resolves its runtime
assets relative to its own executable. `ResolveBinary` in `binary_release.go`
rejects an empty embed at runtime with a clear error.

Do not commit the real (multi-MB) distribution here — `scripts/build-pi.sh`
materializes it per build.

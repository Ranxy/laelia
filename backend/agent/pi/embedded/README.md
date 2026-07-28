# embedded/

This directory holds the standalone **pi** binary that the release build of
laelia embeds via `//go:embed embedded/pi` in `../binary_release.go`.

The committed `pi` file is an **empty placeholder** so the `release`-tagged Go
build compiles on a fresh checkout. It is NOT a runnable binary. Before building
a release, run `scripts/build-pi.sh` to download the real pi binary for the
target `GOOS/GOARCH` and overwrite this file. `ResolveBinary` in
`binary_release.go` rejects an empty embed at runtime with a clear error.

Do not commit a real (multi-MB) binary here — `scripts/build-pi.sh` materializes
it per build.
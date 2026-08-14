package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Print(`Laelia one-click test server launcher.

Usage:
  testserver run    --workdir <dir> [options]   start a test server
  testserver stop   --workdir <dir>             stop a running test server
  testserver status --workdir <dir>             show instance status

Run options:
  --workdir <dir>     work directory (required); all runtime state lives here
  --repo <dir>        repository root (for the build script)
  --port <n>          HTTP port (default: random free port)
  --pg-port <n>       postgres port (default: random free port)
  --host <addr>       bind address (default 127.0.0.1; use 0.0.0.0 to share)
  --no-seed           skip seeding test data
  --build             force rebuild of the laelia binary
  --keep              keep postgres data on exit (debugging)
  --cache <dir>       shared cache dir (default LAELIA_TEST_CACHE or ~/.cache/laelia-test)
  --binary <path>     path to the laelia binary
  --admin-email <e>   admin email (default admin@laelia.test)
  --admin-password <p> admin password (default admin1234)
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var code int
	switch os.Args[1] {
	case "run":
		code = runCmd(os.Args[2:])
	case "stop":
		code = stopCmd(os.Args[2:])
	case "status":
		code = statusCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		code = 0
	default:
		usage()
		code = 2
	}
	os.Exit(code)
}

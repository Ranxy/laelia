//go:build embed_machine

package server

import (
	"embed"
	"io/fs"

	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
)

//go:embed embedded_machine
var embeddedMachine embed.FS

// machineManifest returns the embedded machine manifest.json bytes.
func machineManifest() ([]byte, error) {
	return embeddedMachine.ReadFile("embedded_machine/manifest.json")
}

// openMachineGz opens the gzipped machine binary for the given target
// (e.g. "linux-x64"). The file name comes from the manifest: the embed build
// appends a -no-pi suffix to every artifact when pi is not embedded.
func openMachineGz(target string) (fs.File, error) {
	name := "laelia-machine-" + target + ".gz"
	if manifestName, ok := machinebuild.GzFileName(target); ok {
		name = manifestName
	}
	return embeddedMachine.Open("embedded_machine/" + name)
}

//go:build !embed_machine

package server

import (
	"errors"
	"io/fs"
)

// machineManifest returns an error when machine binaries are not embedded.
func machineManifest() ([]byte, error) {
	return nil, errors.New("machine binaries not embedded; build with embed_machine tag")
}

// openMachineGz returns fs.ErrNotExist when machine binaries are not embedded.
func openMachineGz(_ string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

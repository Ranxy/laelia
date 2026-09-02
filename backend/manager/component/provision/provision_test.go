package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRenderBootstrapScript(t *testing.T) {
	script, err := RenderBootstrapScript(BootstrapParams{
		ManagerURL:   "https://laelia.example.com",
		BinaryTarget: "linux-x64",
		GzSha256:     "gzsha",
		Sha256:       "rawsha",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"#!/bin/sh",
		"set -eu",
		`MANAGER_URL="https://laelia.example.com"`,
		`TARGET="linux-x64"`,
		`WANT_GZ_SHA256="gzsha"`,
		`WANT_SHA256="rawsha"`,
		"curl -fsS \"$MANAGER_URL/machine/manifest.json\"",
		"curl -fsS \"$MANAGER_URL/machine/bin/$TARGET\"",
		"sha256sum -c -",
		"gunzip -c",
		"/bootstrap/machine.json",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("rendered script missing %q", want)
		}
	}

	// The image contract is POSIX sh: no bashisms may creep in (the design's
	// draft used the `<<<` heredoc, which dash rejects).
	for _, bashism := range []string{"<<<", "[[", "function "} {
		if strings.Contains(script, bashism) {
			t.Errorf("rendered script contains bashism %q", bashism)
		}
	}
}

func TestRenderBootstrapScriptValidation(t *testing.T) {
	if _, err := RenderBootstrapScript(BootstrapParams{}); err == nil {
		t.Error("empty params must fail (no manager URL)")
	}
	if _, err := RenderBootstrapScript(BootstrapParams{ManagerURL: "https://x"}); err == nil {
		t.Error("missing binary target must fail")
	}
	if _, err := RenderBootstrapScript(BootstrapParams{ManagerURL: "https://x", BinaryTarget: "t"}); err == nil {
		t.Error("missing checksums must fail")
	}

	// Values are interpolated into a double-quoted shell context: a
	// quote-bearing value would break out of the assignment.
	injected := BootstrapParams{
		ManagerURL:   "https://x\"; rm -rf /; echo \"",
		BinaryTarget: "linux-x64",
		GzSha256:     "gz",
		Sha256:       "raw",
	}
	if _, err := RenderBootstrapScript(injected); err == nil {
		t.Error("quote-bearing manager URL must be rejected")
	}
}

func TestMachineFingerprint(t *testing.T) {
	got := MachineFingerprint("prov-uuid", "machine-uuid")
	if len(got) != 16 {
		t.Fatalf("fingerprint length = %d, want 16", len(got))
	}

	// Locked to the design's derivation: sha256("provisioner:"+prov+":"+machine)[:16].
	h := sha256.Sum256([]byte("provisioner:prov-uuid:machine-uuid"))
	if want := hex.EncodeToString(h[:])[:16]; got != want {
		t.Fatalf("fingerprint = %s, want %s", got, want)
	}

	if MachineFingerprint("prov-uuid", "other") == got {
		t.Error("different machines must not share a fingerprint")
	}
	if MachineFingerprint("other", "machine-uuid") == got {
		t.Error("different provisioners must not share a fingerprint")
	}

	// Stable across calls (the replay path recomputes it).
	if MachineFingerprint("prov-uuid", "machine-uuid") != got {
		t.Error("fingerprint must be deterministic")
	}
}

func TestMachineWorkloadName(t *testing.T) {
	if got := MachineWorkloadName("a1b2c3d4-e5f6-7890-abcd-ef0123456789"); got != "laelia-machine-a1b2c3d4" {
		t.Fatalf("workload name = %q", got)
	}
	// A resource id without dashes passes through whole.
	if got := MachineWorkloadName("abc123"); got != "laelia-machine-abc123" {
		t.Fatalf("workload name = %q", got)
	}
}

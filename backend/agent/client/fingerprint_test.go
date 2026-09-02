package client_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/Ranxy/laelia/backend/agent/client"
)

// TestComputeFingerprintEnvOverride covers the provisioned-pod fingerprint
// contract (design §8.4): LAELIA_FINGERPRINT set wins verbatim over the
// hostname-derived hash, so a pod presents the manager-minted fingerprint even
// as its hostname changes on reschedule; self-hosted hosts (env unset) keep
// the deterministic hostname:os:arch hash.
func TestComputeFingerprintEnvOverride(t *testing.T) {
	hostname, _ := os.Hostname()
	base := client.ComputeFingerprint(hostname, runtime.GOOS, runtime.GOARCH)
	if base == "" {
		t.Fatal("ComputeFingerprint returned an empty fingerprint")
	}
	if client.ComputeFingerprint("other-host", "linux", "amd64") == base && hostname != "other-host" {
		t.Fatal("expected distinct hostnames to produce distinct fingerprints")
	}

	t.Setenv("LAELIA_FINGERPRINT", "1f0a9cdeadbeef01")
	if got := client.ComputeFingerprint("any-host", "linux", "amd64"); got != "1f0a9cdeadbeef01" {
		t.Fatalf("env override not honored verbatim: got %q", got)
	}
	// The override is stable across host property changes — that is the point:
	// the mint-time binding survives pod rescheduling.
	if got := client.ComputeFingerprint("rescheduled-host", "linux", "amd64"); got != "1f0a9cdeadbeef01" {
		t.Fatalf("override must not depend on host properties: got %q", got)
	}
}

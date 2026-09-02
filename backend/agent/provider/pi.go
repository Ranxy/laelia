package provider

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PiProvider discovers a user-installed pi coding agent on the host and
// exposes it as a provider option. It is a non-ACP runtime: laelia drives it
// through the existing pi RPC executor rather than the ACP executor.
type PiProvider struct{}

// MinSupportedPiVersion is the oldest user-installed pi laelia supports. It
// mirrors the version the built-in pi runtime is pinned to so the RPC protocol
// and session format are known-good.
const MinSupportedPiVersion = "0.82.1"

func (*PiProvider) ID() string          { return PiProviderID }
func (*PiProvider) DisplayName() string { return "Pi (user-installed)" }

// IsNonACPRuntime marks pi as a non-ACP provider so ACP config/capability
// builders never derive an ACP launch command for it.
func (*PiProvider) IsNonACPRuntime() bool { return true }

// ToolCallAdapter returns DefaultAdapter; pi is not driven through ACP tool
// frames, so this is never used.
func (*PiProvider) ToolCallAdapter() ToolCallAdapter { return DefaultAdapter{} }

// BuildCommand returns the detected pi executable. It exists to satisfy
// Provider; because PiProvider implements NonACPRuntime, the ACP executor
// never calls it.
func (*PiProvider) BuildCommand(_ string) (string, []string) {
	return "pi", []string{"--mode", "rpc"}
}

// Detect reports whether a compatible pi is installed on PATH. A pi binary is
// treated as present even when it is too old, but Compatible is set false and
// IncompatibilityReason explains why so the UI can show it disabled.
func (p *PiProvider) Detect(ctx context.Context) (*Detected, bool, error) {
	path, err := exec.LookPath("pi")
	if err != nil {
		//nolint:nilerr // pi not on PATH -> provider absent, not a probe error
		return nil, false, nil
	}
	version := runVersionCmd(ctx, "pi", "--version")
	info := &Detected{
		ProviderID:     p.ID(),
		DisplayName:    p.DisplayName(),
		Version:        version,
		ExecutablePath: path,
		Compatible:     true,
	}
	if version == "" {
		info.Compatible = false
		info.IncompatibilityReason = "could not determine pi version"
	} else if !piVersionAtLeast(version, MinSupportedPiVersion) {
		info.Compatible = false
		info.IncompatibilityReason = fmt.Sprintf("requires pi >= %s", MinSupportedPiVersion)
	}
	return info, true, nil
}

// ProbeModels lists the models pi itself knows about (`pi --list-models`).
// These are used by the "use pi's own model/auth" mode. The probe is best
// effort: failures return an empty list and the UI falls back to a free-text
// model input.
func (*PiProvider) ProbeModels(ctx context.Context, _ string) ([]ModelOption, bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "pi", "--list-models")
	out, err := cmd.Output()
	if err != nil {
		return nil, false, err
	}
	var models []ModelOption
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		// pi --list-models can emit a table with a header row like
		// "provider model context max-out thinking images" followed by rows
		// like "deepseek deepseek-v4-flash 1M 384K yes no". Skip the header and
		// turn each row into a "provider/model" id. Plain single-token output
		// (e.g. "anthropic/claude-sonnet-4-5") is kept as-is.
		if strings.EqualFold(fields[0], "provider") {
			continue
		}
		if len(fields) >= 2 {
			value := fields[0] + "/" + fields[1]
			models = append(models, ModelOption{Value: value, Name: fields[1]})
			continue
		}
		models = append(models, ModelOption{Value: line, Name: line})
	}
	if err := sc.Err(); err != nil {
		return nil, false, err
	}
	return models, false, nil
}

var piVersionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

//nolint:unparam
func piVersionAtLeast(version, minVersion string) bool {
	got, ok := parsePiVersion(version)
	if !ok {
		return false
	}
	want, ok := parsePiVersion(minVersion)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if got[i] < want[i] {
			return false
		}
		if got[i] > want[i] {
			return true
		}
	}
	return true
}

func parsePiVersion(version string) ([3]int, bool) {
	m := piVersionRe.FindStringSubmatch(version)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

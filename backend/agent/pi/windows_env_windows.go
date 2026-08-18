//go:build windows

package pi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// windowsEnvironmentScript reads the Machine and User environment scopes as a
// compact JSON object. The daemon may run as a Windows service/background
// process where the process environment (especially PATH) does not include
// user-installed tools; merging these scopes restores them for child processes
// such as pi and the laelia-machine CLI it shells out to.
const windowsEnvironmentScript = `& {
  $result = [ordered]@{}
  foreach ($scope in @('Machine', 'User')) {
    $scopeEnv = [Environment]::GetEnvironmentVariables($scope)
    $scopeObj = [ordered]@{}
    foreach ($key in $scopeEnv.Keys) {
      $value = $scopeEnv[$key]
      if ($null -ne $value) { $scopeObj[$key] = [string]$value }
    }
    $result[$scope] = $scopeObj
  }
  $result | ConvertTo-Json -Compress -Depth 3
}`

// windowsEnvTimeout bounds the PowerShell environment read so a slow/hung
// PowerShell never blocks pi session startup indefinitely.
const windowsEnvTimeout = 5 * time.Second

// windowsPowerShellPath returns a full path to powershell.exe so environment
// merging still works even when the daemon's own PATH is incomplete (e.g. it
// runs as a Windows service with a stripped PATH).
func windowsPowerShellPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to the well-known default location.
	p := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return "powershell.exe"
}

// mergeWindowsUserEnvironment merges the Windows Machine/User environment
// scopes into env (a map of key->value). PATH is merged case-insensitively
// with dedup; other keys are overlaid Machine -> User -> existing process env.
// Failures are silent: the process env is already a usable fallback.
func mergeWindowsUserEnvironment(env map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), windowsEnvTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, windowsPowerShellPath(), "-NoProfile", "-NonInteractive", "-Command", windowsEnvironmentScript).Output()
	if err != nil {
		return
	}
	var parsed struct {
		Machine map[string]string `json:"Machine"`
		User    map[string]string `json:"User"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return
	}

	// Merge non-PATH keys: Machine first, then User, then keep existing.
	layers := []map[string]string{parsed.Machine, parsed.User}
	for _, layer := range layers {
		for k, v := range layer {
			if strings.EqualFold(k, "Path") {
				continue
			}
			setEnvKey(env, k, v)
		}
	}

	// Merge PATH with case-insensitive dedup.
	pathKey := findEnvKey(env, "Path")
	if pathKey == "" {
		pathKey = "Path"
	}
	merged := mergePathValues(
		env[pathKey],
		parsed.Machine["Path"],
		parsed.User["Path"],
	)
	if merged != "" {
		env[pathKey] = merged
	}
}

func findEnvKey(env map[string]string, name string) string {
	for k := range env {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return ""
}

func setEnvKey(env map[string]string, key, value string) {
	existing := findEnvKey(env, key)
	if existing != "" && existing != key {
		delete(env, existing)
	}
	env[key] = value
}

func mergePathValues(values ...string) string {
	seen := map[string]bool{}
	segments := []string{}
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, raw := range strings.Split(value, ";") {
			seg := strings.TrimSpace(raw)
			if seg == "" {
				continue
			}
			lower := strings.ToLower(seg)
			if seen[lower] {
				continue
			}
			seen[lower] = true
			segments = append(segments, seg)
		}
	}
	return strings.Join(segments, ";")
}

//go:build !windows

package pi

// mergeWindowsUserEnvironment is a no-op on non-Windows platforms.
func mergeWindowsUserEnvironment(_ map[string]string) {}

//go:build !windows

package providers

func readWindowsUserEnv(name string) string {
	return ""
}

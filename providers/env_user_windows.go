//go:build windows

package providers

import (
	"golang.org/x/sys/windows/registry"
)

// readWindowsUserEnv returns a per-user environment value from the registry (HKCU\Environment).
// Values set in "系统属性 → 环境变量" are stored here immediately, but processes started before
// the change (including many IDE terminals) may not see them via os.Getenv until restart.
func readWindowsUserEnv(name string) string {
	if name == "" {
		return ""
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = k.Close() }()
	val, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return val
}

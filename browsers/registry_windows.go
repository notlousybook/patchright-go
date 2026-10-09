//go:build windows

package browsers

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// appPathKeys maps browser exe names to look up under App Paths.
func appPathKeys() []string {
	var keys []string
	for _, b := range Supported {
		for _, exe := range candidatesFor(b.ID) {
			if strings.HasSuffix(strings.ToLower(exe), ".exe") {
				keys = append(keys, exe)
			}
		}
	}
	return keys
}

func searchRegistryImpl() []Found {
	var out []Found
	keys := appPathKeys()
	roots := []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER}
	for _, root := range roots {
		for _, exe := range keys {
			sub := `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + exe
			k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			val, _, err := k.GetStringValue("")
			k.Close()
			if err != nil || val == "" {
				continue
			}
			val = strings.Trim(val, `"`)
			if !fileExists(val) {
				continue
			}
			b, ok := matchBrowserByPath(val)
			if !ok {
				// match by the registered exe name itself.
				b, ok = matchBrowserByPath(filepath.Base(val))
				if !ok {
					continue
				}
			}
			out = append(out, Found{Browser: b, Path: val, Source: "registry", Uses: usageScore(val, b.ID, "registry")})
		}
	}
	return out
}

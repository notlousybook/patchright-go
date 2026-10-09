package browsers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// searchShortcuts resolves browser executables referenced by desktop/start
// menu shortcuts: .lnk on windows, .desktop on linux, .app bundles on mac
// (those are covered by installDirs, but we also scan them here for names).
func searchShortcuts() []Found {
	var out []Found
	for _, dir := range shortcutDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			if e.IsDir() {
				// start menu subfolders (e.g. "Google Chrome/").
				out = append(out, searchShortcutsIn(full)...)
				continue
			}
			if f := matchShortcutFile(full); f != nil {
				out = append(out, *f)
			}
		}
	}
	return out
}

func searchShortcutsIn(dir string) []Found {
	var out []Found
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if f := matchShortcutFile(filepath.Join(dir, e.Name())); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// matchShortcutFile checks one shortcut file and resolves it to a browser
// executable when it matches a supported browser.
func matchShortcutFile(path string) *Found {
	lower := strings.ToLower(filepath.Base(path))
	var target string
	switch runtime.GOOS {
	case "windows":
		if !strings.HasSuffix(lower, ".lnk") {
			return nil
		}
		target = resolveLnk(path)
	case "darwin":
		return nil // .app bundles handled via install dirs
	default:
		if !strings.HasSuffix(lower, ".desktop") {
			return nil
		}
		target = resolveDesktop(path)
	}
	if target == "" {
		return nil
	}
	b, ok := matchBrowserByPath(target)
	if !ok {
		// fall back to matching the shortcut's own file name.
		b, ok = matchBrowserByName(filepath.Base(path))
		if !ok {
			return nil
		}
	}
	if !fileExists(target) {
		return nil
	}
	return &Found{Browser: b, Path: target, Source: "shortcut", Uses: usageScore(target, b.ID, "shortcut")}
}

// matchBrowserByPath matches an executable path to a supported browser by
// file name.
func matchBrowserByPath(target string) (Browser, bool) {
	base := strings.ToLower(filepath.Base(target))
	for _, b := range Supported {
		for _, exe := range candidatesFor(b.ID) {
			if strings.ToLower(exe) == base {
				return b, true
			}
			// mac subpath match: ".../Google Chrome" binary name.
			if strings.HasSuffix(strings.ToLower(target), strings.ToLower(exe)) {
				return b, true
			}
		}
		// loose: executable name contains the id (covers e.g. chrome_proxy).
		for _, n := range b.Names {
			simple := strings.ToLower(strings.ReplaceAll(n, " ", ""))
			if strings.Contains(strings.ReplaceAll(base, " ", ""), simple) {
				return b, true
			}
		}
	}
	return Browser{}, false
}

// matchBrowserByName matches a shortcut file name to a browser.
func matchBrowserByName(name string) (Browser, bool) {
	lower := strings.ToLower(name)
	for _, b := range Supported {
		for _, n := range b.Names {
			if strings.Contains(lower, strings.ToLower(n)) {
				return b, true
			}
		}
	}
	return Browser{}, false
}

// resolveLnk extracts the target path from a Windows .lnk file. .lnk is a
// binary format; we parse the LinkTargetIDList / LinkInfo sections enough to
// pull out a local path (drive-letter or UNC). Falls back to scanning for a
// plausible .exe path embedded in the blob.
func resolveLnk(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 76 {
		return ""
	}
	// sanity: .lnk header size is 76 (0x4C) and magic GUID present.
	if data[0] != 0x4C || data[1] != 0x00 || data[2] != 0x00 || data[3] != 0x00 {
		return ""
	}
	// LinkFlags at 0x14; bit 1 (HasLinkTargetIDList), bit 7 (HasLinkInfo).
	flags := int(data[0x14]) | int(data[0x15])<<8 | int(data[0x16])<<16 | int(data[0x17])<<24
	off := 76
	if flags&0x02 != 0 {
		if off+2 > len(data) {
			return ""
		}
		size := int(data[off]) | int(data[off+1])<<8
		off += size
	}
	if flags&0x80 != 0 && off < len(data) {
		if target := parseLinkInfo(data[off:]); target != "" {
			return target
		}
	}
	// fallback: scan for an .exe path (ASCII or UTF-16).
	if target := scanExePath(data); target != "" {
		return target
	}
	return ""
}

// parseLinkInfo pulls a local/UNC path out of a LinkInfo blob.
func parseLinkInfo(blob []byte) string {
	if len(blob) < 28 {
		return ""
	}
	total := int(blob[0]) | int(blob[1])<<8 | int(blob[2])<<16 | int(blob[3])<<24
	if total > len(blob) || total < 0 {
		total = len(blob)
	}
	blob = blob[:total]
	headerSize := int(blob[4]) | int(blob[5])<<8 | int(blob[6])<<16 | int(blob[7])<<24
	linkInfoFlags := int(blob[8]) | int(blob[9])<<8 | int(blob[10])<<16 | int(blob[11])<<24
	// Offsets (present when headerSize >= 36).
	localOff := -1
	if headerSize >= 36 && len(blob) >= 32 {
		localOff = int(blob[28]) | int(blob[29])<<8 | int(blob[30])<<16 | int(blob[31])<<24
	}
	extract := func(at int) string {
		if at < 0 || at >= len(blob) {
			return ""
		}
		end := at
		for end < len(blob) && blob[end] != 0 {
			end++
		}
		s := string(blob[at:end])
		if looksLikeExePath(s) {
			return s
		}
		// try UTF-16.
		var sb strings.Builder
		for i := at; i+1 < len(blob); i += 2 {
			if blob[i] == 0 && blob[i+1] == 0 {
				break
			}
			if blob[i+1] == 0 {
				sb.WriteByte(blob[i])
			} else {
				return ""
			}
		}
		if looksLikeExePath(sb.String()) {
			return sb.String()
		}
		return ""
	}
	_ = linkInfoFlags
	if s := extract(localOff); s != "" {
		return s
	}
	return ""
}

// scanExePath hunts for something shaped like C:\...\foo.exe in a blob,
// ASCII or UTF-16LE.
func scanExePath(data []byte) string {
	best := ""
	// ASCII scan.
	for i := 0; i+8 < len(data); i++ {
		if data[i+1] != ':' || data[i+2] != '\\' {
			continue
		}
		j := i
		for j < len(data) && data[j] >= 32 && data[j] < 127 && data[j] != '"' {
			j++
		}
		s := string(data[i:j])
		if looksLikeExePath(s) && len(s) > len(best) {
			best = s
		}
	}
	// UTF-16LE scan: letter, 0, ':', 0, '\', 0 ...
	for i := 0; i+12 < len(data); i += 2 {
		if data[i+1] != 0 || data[i+2] != ':' || data[i+3] != 0 || data[i+4] != '\\' || data[i+5] != 0 {
			continue
		}
		var sb strings.Builder
		for j := i; j+1 < len(data); j += 2 {
			if data[j+1] != 0 {
				break
			}
			if data[j] == 0 {
				break
			}
			sb.WriteByte(data[j])
			if sb.Len() > 520 {
				break
			}
		}
		s := sb.String()
		if looksLikeExePath(s) && len(s) > len(best) {
			best = s
		}
	}
	return best
}

func looksLikeExePath(s string) bool {
	if len(s) < 8 || len(s) > 520 {
		return false
	}
	lower := strings.ToLower(s)
	if !strings.HasSuffix(lower, ".exe") {
		return false
	}
	// drive path or UNC.
	if len(s) > 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	if strings.HasPrefix(s, `\\`) {
		return true
	}
	return false
}

// resolveDesktop parses a freedesktop .desktop file's Exec= line.
func resolveDesktop(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		exec := strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
		// strip field codes (%U %F %u ...) and take the binary.
		fields := strings.Fields(exec)
		for _, f := range fields {
			if strings.HasPrefix(f, "%") {
				continue
			}
			f = strings.Trim(f, `"'`)
			if f == "" {
				continue
			}
			// bare name -> resolve via PATH; absolute -> check directly.
			if filepath.IsAbs(f) {
				if fileExists(f) {
					return f
				}
				continue
			}
			if p, err := lookPath(f); err == nil {
				return p
			}
		}
	}
	return ""
}

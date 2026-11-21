package detect

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Library is what this computer has installed, gathered once per scan:
// Steam library folders, Wine prefixes and launcher install folders.
type Library struct {
	Steam    []string // steamapps/common folders
	DriveC   []string // drive_c folders of Wine and Proton prefixes
	Installs []string // game folders launchers report as installed
}

// Has reports whether a game is installed: one of its install paths exists,
// or its folder sits in a Steam library (steamDir, "" for none).
//
// The catalog's install paths are Windows paths ("C:\Riot Games\VALORANT").
// Outside Windows such a path is looked up inside every Wine prefix's
// drive_c (C: paths only; Wine keeps other drives as links elsewhere), with
// folder names matched case-insensitively as Windows does, and its last
// folder is matched against launcher install folders, which launchers name
// after the game's own folder ("rocketleague").
func (l Library) Has(paths []string, steamDir string) bool {
	for _, p := range paths {
		drive, parts, ok := splitWindowsPath(p)
		if !ok || filepath.VolumeName(p) != "" {
			// A path for this OS (on Windows, Windows paths are).
			if _, err := os.Stat(p); err == nil {
				return true
			}
			continue
		}
		if l.hasWindowsPath(drive, parts) {
			return true
		}
	}
	return len(l.SteamInstallDirs(0, steamDir)) != 0
}

// SteamInstallDirs prefers the per-AppID manifests, which know the actual
// install directory even when the catalog's default name has changed.
func (l Library) SteamInstallDirs(appID int, defaultDir string) []string {
	var dirs []string
	for _, lib := range l.Steam {
		if appID > 0 {
			if dir := manifestInstallDir(lib, appID); dir != "" {
				dirs = append(dirs, dir)
				continue
			}
		}
		if defaultDir != "" {
			if dir := filepath.Join(lib, defaultDir); isDir(dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}

var appManifestField = regexp.MustCompile(`(?im)^[ \t]*"(appid|StateFlags|installdir)"[ \t]+"([^"\r\n]+)"[ \t\r]*$`)

func manifestInstallDir(lib string, appID int) string {
	id := strconv.Itoa(appID)
	file, err := os.Open(filepath.Join(filepath.Dir(lib), "appmanifest_"+id+".acf"))
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<10))
	if err != nil {
		return ""
	}
	fields := make(map[string]string, 3)
	for _, item := range appManifestField.FindAllStringSubmatch(string(data), 3) {
		fields[strings.ToLower(item[1])] = item[2]
	}
	flags, err := strconv.ParseUint(fields["stateflags"], 10, 32)
	const fullyInstalled = 4
	name := fields["installdir"]
	if fields["appid"] != id || err != nil || flags&fullyInstalled == 0 ||
		name == "" || name == "." || !filepath.IsLocal(name) || strings.ContainsAny(name, `\/:`) {
		return ""
	}
	dir := filepath.Join(lib, name)
	if !isDir(dir) {
		return ""
	}
	return dir
}

// hasWindowsPath looks for a Windows path, split by splitWindowsPath, in the
// Wine prefixes and launcher installs.
func (l Library) hasWindowsPath(drive byte, parts []string) bool {
	if len(parts) == 0 {
		return false // a bare drive is no game
	}
	if drive == 'c' {
		for _, dc := range l.DriveC {
			if existsFold(dc, parts) {
				return true
			}
		}
	}
	last := parts[len(parts)-1]
	for _, in := range l.Installs {
		if strings.EqualFold(filepath.Base(in), last) && isDir(in) {
			return true
		}
	}
	return false
}

// splitWindowsPath splits "C:\Riot Games\VALORANT" into 'c' and its
// folder names.
func splitWindowsPath(p string) (drive byte, parts []string, ok bool) {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return 0, nil, false
	}
	drive = p[0] | 0x20
	if drive < 'a' || drive > 'z' {
		return 0, nil, false
	}
	for _, s := range strings.FieldsFunc(p[3:], func(r rune) bool { return r == '\\' || r == '/' }) {
		if s == "." || s == ".." {
			return 0, nil, false // catalog paths are plain; never climb out of drive_c
		}
		parts = append(parts, s)
	}
	return drive, parts, true
}

// existsFold reports whether root/parts... exists, matching each name
// without regard to case, as Wine does when a Windows program opens it.
func existsFold(root string, parts []string) bool {
	cur := root
	for _, part := range parts {
		next := filepath.Join(cur, part)
		if _, err := os.Stat(next); err == nil {
			cur = next
			continue
		}
		ents, err := os.ReadDir(cur)
		if err != nil {
			return false
		}
		found := false
		for _, e := range ents {
			if strings.EqualFold(e.Name(), part) {
				cur, found = filepath.Join(cur, e.Name()), true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

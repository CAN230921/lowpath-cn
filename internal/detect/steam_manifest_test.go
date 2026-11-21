package detect

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestSteamInstallDirsUsesMatchingManifest(t *testing.T) {
	first := mkdir(t, t.TempDir(), "steamapps", "common")
	second := mkdir(t, t.TempDir(), "steamapps", "common")
	actual := mkdir(t, first, "Renamed Game")
	fallback := mkdir(t, second, "Original Name")
	lib := Library{Steam: []string{first, second}}
	manifest := filepath.Join(filepath.Dir(first), "appmanifest_123.acf")
	write(t, manifest, `"AppState" {
  "appid" "123"
  "StateFlags" "6"
  "installdir" "Renamed Game"
}`)
	if got := lib.SteamInstallDirs(123, "Original Name"); !slices.Equal(got, []string{actual, fallback}) {
		t.Fatalf("SteamInstallDirs = %v, want the manifest directory and existing fallback", got)
	}
	write(t, manifest, `"AppState" {
  "appid" "456"
  "StateFlags" "4"
  "installdir" "Renamed Game"
}`)
	if got := lib.SteamInstallDirs(123, "Original Name"); !slices.Equal(got, []string{fallback}) {
		t.Fatalf("mismatched AppID produced %v", got)
	}
	write(t, manifest, `"AppState" {
  "appid" "123"
  "StateFlags" "2"
  "installdir" "Renamed Game"
}`)
	if got := lib.SteamInstallDirs(123, ""); len(got) != 0 {
		t.Fatalf("incomplete install produced %v", got)
	}
	write(t, manifest, `"AppState" {
  "appid" "123"
  "StateFlags" "4"
  "installdir" ".."
}`)
	if got := lib.SteamInstallDirs(123, ""); len(got) != 0 {
		t.Fatalf("path outside the Steam common directory produced %v", got)
	}
}

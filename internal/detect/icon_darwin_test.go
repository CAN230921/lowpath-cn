//go:build cgo

package detect

import "testing"

func TestMacAppIcon(t *testing.T) {
	icon, err := exeIcon("/System/Applications/Chess.app/Contents/MacOS/Chess", 48)
	if err != nil {
		t.Fatal(err)
	}
	if icon.Bounds().Dx() != 48 || icon.Bounds().Dy() != 48 {
		t.Fatalf("want a 48x48 icon, got %v", icon.Bounds())
	}
	if _, err := exeIcon("/usr/bin/false", 48); err == nil {
		t.Fatal("plain executables should fall back to catalog art")
	}
}

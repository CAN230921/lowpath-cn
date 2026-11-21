package app

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestArtFromSteamCache(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "appcache", "librarycache", "730")
	if err := os.MkdirAll(filepath.Join(lib, "abc123"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Steam keeps some files in the app folder and some one level below it.
	os.WriteFile(filepath.Join(lib, "library_hero.jpg"), []byte("hero"), 0o644)
	os.WriteFile(filepath.Join(lib, "abc123", "header.jpg"), []byte("header"), 0o644)

	fp := newFake(t)
	a := newArtStore("", nil, fp.Games, fp.Icons)
	a.steam = func() []string { return []string{root} }
	a.libs = func() []string { return nil }
	g := Game{ID: "cs2", SteamApp: 730}

	for kind, want := range map[string]string{"hero": "hero", "header": "header"} {
		p, err := a.path(g, kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if b, _ := os.ReadFile(p); string(b) != want {
			t.Fatalf("%s: got %q", kind, b)
		}
	}
	// Not cached locally and no cache dir to download into: a miss, remembered.
	if _, err := a.path(g, "logo"); err != errNoArt {
		t.Fatalf("logo: want errNoArt, got %v", err)
	}
	if _, ok := a.miss["cs2_logo"]; !ok {
		t.Fatal("miss not remembered")
	}
	if _, err := a.path(g, "bogus"); err != errNoArt {
		t.Fatalf("unknown kind: %v", err)
	}
	// A game without a Steam id and no program on disk has no art at all.
	if _, err := a.path(Game{ID: "custom-x", Processes: []string{"no-such-program.exe"}}, "icon"); err != errNoArt {
		t.Fatalf("icon: %v", err)
	}
}

// A game's icon comes from its running program through the platform's
// detector and icon reader, and is cached as a PNG.
func TestArtIconFromRunningProgram(t *testing.T) {
	fp := newFake(t)
	exe := filepath.Join(t.TempDir(), "mygame.exe")
	os.WriteFile(exe, nil, 0o644)
	fp.Games.SetRunning("mygame.exe")
	fp.Games.SetExe("mygame.exe", exe)
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(1, 1, color.NRGBA{255, 0, 0, 255})
	fp.Icons.Set(exe, img)

	a := newArtStore(t.TempDir(), discard, fp.Games, fp.Icons)
	g := Game{ID: "custom-mygame", Processes: []string{"mygame.exe"}}
	p, err := a.path(g, "icon")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := got.At(1, 1).RGBA(); r != 0xffff {
		t.Errorf("cached icon pixel %v, want the program's red", got.At(1, 1))
	}

	// Without an icon reader that knows the program: no art, not a crash.
	if _, err := newArtStore(t.TempDir(), discard, fp.Games, newFake(t).Icons).path(g, "icon"); err != errNoArt {
		t.Errorf("unknown icon: %v, want errNoArt", err)
	}
}

func TestSteamHostAllowList(t *testing.T) {
	for h, ok := range map[string]bool{
		"shared.akamai.steamstatic.com":  true,
		"cdn.cloudflare.steamstatic.com": true,
		"store.steampowered.com":         true,
		"steamstatic.com.evil.example":   false,
		"evilsteamstatic.com":            false,
	} {
		if steamHost(h) != ok {
			t.Errorf("%s: want %v", h, ok)
		}
	}
}

type artTransport func(*http.Request) (*http.Response, error)

func (f artTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestPublisherIconCachesRealImageType(t *testing.T) {
	fp := newFake(t)
	dir := t.TempDir()
	a := newArtStore(dir, discard, fp.Games, fp.Icons)
	g := Game{ID: "publisher-test", art: map[string]string{"icon": "https://cmsassets.rgpub.io/publisher-icon.jpg"}}
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := jpeg.Encode(&data, img, nil); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a.client.Transport = artTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != g.art["icon"] {
			t.Fatalf("unexpected icon URL %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data.Bytes()))}, nil
	})
	p, err := a.path(g, "icon")
	if err != nil || filepath.Ext(p) != ".jpg" || calls != 1 {
		t.Fatalf("publisher icon: %s, %v, requests=%d", p, err, calls)
	}
	reopened := newArtStore(dir, discard, fp.Games, fp.Icons)
	reopened.client.Transport = a.client.Transport
	if got, err := reopened.path(g, "icon"); err != nil || got != p || calls != 1 {
		t.Fatalf("reopened cache: %s, %v, requests=%d", got, err, calls)
	}
	custom := Game{ID: "custom-art-test", Custom: true, SteamApp: 730, art: g.art}
	if _, err := reopened.path(custom, "icon"); err != errNoArt || calls != 1 {
		t.Fatalf("custom game made a remote request: %v, requests=%d", err, calls)
	}
}

func TestCatalogArtworkUsesTrustedSources(t *testing.T) {
	games, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, game := range games {
		for kind, source := range game.art {
			u, err := url.Parse(source)
			_, known := artKinds[kind]
			if !known || err != nil || !validArtURL(u) {
				t.Errorf("%s has invalid %s artwork", game.ID, kind)
			}
		}
	}
}

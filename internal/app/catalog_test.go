package app

import (
	"strings"
	"testing"
)

func TestBuiltinCatalogIdentifiers(t *testing.T) {
	games, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	apps := map[int]string{}
	processes := map[string]string{}
	for _, game := range games {
		if game.ID == "" || ids[game.ID] {
			t.Errorf("empty or duplicate game ID %q", game.ID)
		}
		ids[game.ID] = true
		if game.SteamApp > 0 {
			if other := apps[game.SteamApp]; other != "" {
				t.Errorf("Steam app %d is used by %s and %s", game.SteamApp, other, game.ID)
			}
			apps[game.SteamApp] = game.ID
		}
		for _, name := range game.Processes {
			key := strings.ToLower(name)
			if other := processes[key]; other != "" {
				t.Errorf("process %s is used by %s and %s", name, other, game.ID)
			}
			processes[key] = game.ID
		}
	}
}

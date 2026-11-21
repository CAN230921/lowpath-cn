package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

//go:embed index.html
var home string

//go:embed game.js
var gameScript string

var puzzleZone = time.FixedZone("UTC+8", 8*60*60)

const gameSide = 8

func mountGame(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		gameHeaders(w, r, "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		_, _ = io.WriteString(w, home)
	})
	mux.HandleFunc("GET /game.js", func(w http.ResponseWriter, r *http.Request) {
		gameHeaders(w, r, "text/javascript; charset=utf-8")
		_, _ = io.WriteString(w, gameScript)
	})
	mux.HandleFunc("GET /api/game/daily", func(w http.ResponseWriter, r *http.Request) {
		gameHeaders(w, r, "application/json")
		day := time.Now().In(puzzleZone)
		seed, _ := strconv.Atoi(day.Format("20060102"))
		_ = json.NewEncoder(w).Encode(struct {
			Date string `json:"date"`
			Seed int    `json:"seed"`
		}{Date: day.Format(time.DateOnly), Seed: seed})
	})
	mux.HandleFunc("GET /api/game/event", gameEvent)
}

func gameEvent(w http.ResponseWriter, r *http.Request) {
	gameHeaders(w, r, "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if len(r.URL.RawQuery) > 24 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	from, fromErr := strconv.Atoi(query.Get("from"))
	to, toErr := strconv.Atoi(query.Get("to"))
	if err != nil || fromErr != nil || toErr != nil || from < 0 || to < 0 || from >= gameSide*gameSide || to >= gameSide*gameSide {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	delta := to - from
	sameRow := from/gameSide == to/gameSide
	if delta != gameSide && delta != -gameSide && !(sameRow && (delta == 1 || delta == -1)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Scatter each event across four spaces, with the daily board deciding which are walkable.
	switch to % 16 {
	case 4:
		_, _ = io.WriteString(w, `{"event":"bell","duration_ms":480}`)
	case 9:
		_, _ = io.WriteString(w, `{"event":"ferry","duration_ms":650}`)
	case 14:
		_, _ = io.WriteString(w, `{"event":"rain","duration_ms":520}`)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func gameHeaders(w http.ResponseWriter, r *http.Request, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.ProtoMajor < 3 {
		w.Header().Set("Alt-Svc", `h3=":443"; ma=86400`)
	}
}

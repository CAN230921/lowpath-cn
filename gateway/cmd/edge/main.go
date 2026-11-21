package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
	"golang.org/x/crypto/acme/autocert"
)

const relayTarget = "127.0.0.1:42180"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	host, credentials, state := os.Getenv("EDGE_DOMAIN"), os.Getenv("CREDENTIALS_DIRECTORY"), os.Getenv("STATE_DIRECTORY")
	if host == "" || credentials == "" || state == "" {
		return errors.New("missing domain or systemd-managed state and credential directories")
	}
	raw, err := os.ReadFile(filepath.Join(credentials, "access-token"))
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) != 64 {
		return errors.New("invalid access token length")
	}
	digest := sha256.Sum256([]byte("Bearer " + token))

	var regional *routes
	if devices := os.Getenv("EDGE_DEVICE_DIR"); devices != "" {
		regional, err = loadRoutes(filepath.Join(credentials, "routes"), devices)
		if err != nil {
			return err
		}
		if err := regional.configureGitHub(host, filepath.Join(credentials, "github-oauth"), filepath.Join(state, "oauth-devices")); err != nil {
			return err
		}
	}

	proxy := &masque.Proxy{}
	defer proxy.Close()
	handler := newHandler(host, digest, proxy, regional)
	certs := &autocert.Manager{
		Prompt: autocert.AcceptTOS, Cache: autocert.DirCache(state),
		HostPolicy: autocert.HostWhitelist(host),
	}
	tcpTLS := certs.TLSConfig()
	tcpTLS.MinVersion = tls.VersionTLS13
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
	plain := &http.Server{
		Handler: certs.HTTPHandler(redirect), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	https := &http.Server{
		TLSConfig: tcpTLS, Handler: handler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	h3 := &http3.Server{
		TLSConfig:       &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: certs.GetCertificate},
		QUICConfig:      &quic.Config{EnableDatagrams: true, MaxIdleTimeout: 90 * time.Second},
		EnableDatagrams: true, Handler: handler, MaxHeaderBytes: 16 << 10,
	}
	defer plain.Close()
	defer https.Close()
	defer h3.Close()
	httpListener, err := net.Listen("tcp4", ":80")
	if err != nil {
		return err
	}
	defer httpListener.Close()
	httpsListener, err := net.Listen("tcp4", ":443")
	if err != nil {
		return err
	}
	defer httpsListener.Close()
	quicListener, err := net.ListenPacket("udp4", ":443")
	if err != nil {
		return err
	}
	defer quicListener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if regional != nil {
		regional.monitor(ctx)
	}
	fail := make(chan error, 3)
	go func() { fail <- plain.Serve(httpListener) }()
	go func() { fail <- https.ServeTLS(httpsListener, "", "") }()
	go func() { fail <- h3.Serve(quicListener) }()
	log.Printf("listening on HTTPS and HTTP/3 for %s", host)
	select {
	case <-ctx.Done():
		return nil
	case err := <-fail:
		return fmt.Errorf("listener stopped: %w", err)
	}
}

func newHandler(host string, token [32]byte, proxy *masque.Proxy, regional *routes) http.Handler {
	template := uritemplate.MustNew("https://" + host + "/udp/{target_host}/{target_port}/")
	slots := make(chan struct{}, 32)
	mux := http.NewServeMux()
	mountGame(mux)
	mux.HandleFunc("CONNECT /udp/", func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 3 || r.TLS == nil {
			http.NotFound(w, r)
			return
		}
		presented := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(presented[:], token[:]) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		req, err := masque.ParseProxyRequest(r, template)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Target != relayTarget {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = proxy.Proxy(w, req)
	})
	if regional != nil {
		regional.mount(mux, host)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host && r.Host != host+":443" {
			w.WriteHeader(http.StatusMisdirectedRequest)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

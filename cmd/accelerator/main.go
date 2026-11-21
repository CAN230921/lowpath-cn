package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"lowpath/cmd/lowpath/winres"
	"lowpath/internal/app"
	"lowpath/internal/capture"
	"lowpath/internal/managed"
	"lowpath/internal/platform"
	"lowpath/ui"
)

// Packaged deployments can set connection defaults at build time. Flags still
// override the defaults, and ordinary source builds require a gateway URL.
var defaultGateway, defaultGatewayIP, defaultUplink string

func main() {
	if isRouteController() {
		if err := runRouteController(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		if runtime.GOOS == "windows" && flag.Lookup("headless").Value.String() == "false" {
			platform.NewDesktop(platform.DesktopConfig{}).ShowError("Lowpath", "无法启动加速器。\n\n"+err.Error())
		}
		log.Fatal(err)
	}
}

func run() error {
	gateway := flag.String("gateway", firstGateway(), "HTTPS MASQUE gateway origin")
	ip := flag.String("gateway-ip", defaultGatewayIP, "optional IPv4 override, retaining hostname and certificate checks")
	uplink := flag.String("uplink", defaultUplink, "macOS gateway interface: auto, an interface name, or empty for the system route")
	data := flag.String("data", "", "desktop settings directory")
	targetFlag := flag.String("test-target", "", "dedicated UDP test server IPv4:port")
	route := flag.Bool("test-route", false, "route only the dedicated test host via macOS utun (administrator required)")
	headless := flag.Bool("headless", false, "print one-time UI launch URL without opening a window")
	credential := flag.Bool("device-stdin", false, "read an existing device credential from standard input")
	flag.Parse()
	var target netip.AddrPort
	var err error
	if *targetFlag != "" {
		target, err = netip.ParseAddrPort(*targetFlag)
		if err != nil || !target.Addr().Is4() || target.Port() == 0 {
			return errors.New("test-target must be IPv4:port")
		}
	}
	if *route && !target.IsValid() {
		return errors.New("test-route requires test-target")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *route {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
	}
	const testID = "udp-test"
	network, err := managed.New(*gateway, *ip, *uplink, testID, target)
	if err != nil {
		return err
	}
	if *credential {
		in := bufio.NewScanner(os.Stdin)
		if !in.Scan() {
			return errors.New("missing device credential on standard input")
		}
		if err := network.Login(ctx, strings.TrimSpace(in.Text()), "设备登录"); err != nil {
			return err
		}
	}
	if !ui.Built() {
		return errors.New("build the React interface with npm ci and npm run build in ui first")
	}
	assets, err := ui.FS()
	if err != nil {
		return err
	}
	games, err := app.LoadCatalog()
	if err != nil {
		return err
	}
	if target.IsValid() {
		games = append([]app.Game{{ID: testID, Name: "UDP 线路实测", Genre: "测试", Color: "#29c4a7", Processes: []string{"udp-test"}}}, games...)
	}
	if *data == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*data = filepath.Join(base, "LowpathCN")
	}
	store, err := app.OpenStore(filepath.Join(*data, "state.json"))
	if err != nil {
		return err
	}
	plat := platform.Native()
	if *route {
		plat.Capture, err = testCapture(target, *ip)
		if err != nil {
			return err
		}
		if err := plat.Capture.Available(); err != nil {
			return err
		}
		settings := store.Settings()
		settings.CaptureMode = app.CaptureRoute
		if err := store.SetSettings(settings); err != nil {
			return err
		}
	}
	desktop := platform.NewDesktop(platform.DesktopConfig{ProfileDir: filepath.Join(*data, "webview"), Icon: winres.Icon})
	login := &managed.Login{Network: network}
	if !*credential && !*route {
		login.Store = network.SystemCredentialStore()
	}
	cfg := app.Config{
		Store: store, Games: games, Network: network, Platform: plat,
		UI: assets, ArtDir: filepath.Join(*data, "art"),
		Marker: capture.NewMarkerFile(filepath.Join(*data, "capture.json")), ExtraAPI: login.Mount, Quit: stop,
	}
	if plat.OS == "windows" && plat.Privileges.CanElevate() && !*headless && !*credential {
		cfg.Elevate = func() error { return plat.Privileges.Relaunch(os.Args[1:]) }
	}
	svc, err := app.NewServer(cfg)
	if err != nil {
		return err
	}
	defer svc.Close()
	login.StopBoost = svc.StopBoost
	svc.Run(ctx)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: svc.Handler()}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	launch := "http://" + listener.Addr().String() + svc.LaunchPath()
	if *headless {
		fmt.Println(launch)
	} else {
		err := desktop.RunWindow(ctx, launch)
		if err == nil {
			return nil
		}
		if err := desktop.OpenBrowser("http://" + listener.Addr().String() + svc.NewLaunchPath()); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return err
	}
}

func firstGateway() string {
	if value := os.Getenv("LOWPATH_GATEWAY"); value != "" {
		return value
	}
	return defaultGateway
}

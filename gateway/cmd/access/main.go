package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"lowpath-masque-edge/internal/access"
)

const defaultDir = "/etc/lowpath-access/devices"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: lowpath-access issue|revoke [options]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("dir", defaultDir, "device registry directory")
	switch args[0] {
	case "issue":
		label := flags.String("label", "", "device label")
		nodes := flags.String("nodes", "", "comma-separated node IDs")
		ttl := flags.Duration("ttl", 30*24*time.Hour, "credential lifetime")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return issue(*dir, strings.TrimSpace(*label), strings.Split(*nodes, ","), *ttl)
	case "revoke":
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 || !access.ValidDevice(flags.Arg(0)) {
			return errors.New("specify the device ID to revoke")
		}
		return os.Remove(access.Path(*dir, flags.Arg(0)))
	default:
		return errors.New("usage: lowpath-access issue|revoke [options]")
	}
}

func issue(dir, label string, nodes []string, ttl time.Duration) error {
	if label == "" || len(label) > 96 || ttl <= 0 || ttl > 365*24*time.Hour {
		return errors.New("specify a device label (max 96 bytes) and lifetime of at most 365 days")
	}
	for _, node := range nodes {
		if !access.ValidNode(node) {
			return errors.New("specify valid comma-separated node IDs")
		}
	}
	slices.Sort(nodes)
	nodes = slices.Compact(nodes)
	var secret [32]byte
	var idBytes [12]byte
	rand.Read(idBytes[:])
	rand.Read(secret[:])
	id := hex.EncodeToString(idBytes[:])
	value := hex.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(value))
	dev := access.Device{Label: label, Digest: hex.EncodeToString(digest[:]), Nodes: nodes, ExpiresAt: time.Now().UTC().Add(ttl)}

	file, err := os.CreateTemp(dir, ".device-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(dev); err != nil {
		return err
	}
	if err := file.Chmod(0644); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), access.Path(dir, id)); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return err
	}
	fmt.Println(id + "." + value)
	return nil
}

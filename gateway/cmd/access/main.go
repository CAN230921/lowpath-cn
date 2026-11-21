package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"
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
		return errors.New("usage: lowpath-access create-user|issue|revoke [options]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dir := flags.String("dir", defaultDir, "device registry directory")
	switch args[0] {
	case "create-user":
		username := flags.String("username", "", "account name")
		nodes := flags.String("nodes", "", "comma-separated node IDs")
		accounts := flags.String("account-dir", access.DefaultAccountDir, "account registry directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *username == "" || *nodes == "" {
			return errors.New("usage: sudo lowpath-access create-user -username NAME -nodes NODE[,NODE]")
		}
		return createUser(*accounts, *username, strings.Split(*nodes, ","))
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
		return errors.New("usage: lowpath-access create-user|issue|revoke [options]")
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
	value, err := access.IssueDevice(dir, access.Device{Label: label, Nodes: nodes, ExpiresAt: time.Now().UTC().Add(ttl)}, 0644)
	if err != nil {
		return err
	}
	fmt.Println(value)
	return nil
}

func createUser(dir, name string, nodes []string) error {
	if os.Geteuid() != 0 {
		return errors.New("only the gateway administrator can create an account")
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("use an interactive administrator terminal to enter the password")
	}
	fmt.Fprint(os.Stderr, "Password (12–256 bytes, input hidden): ")
	password, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	defer clear(password)
	if !access.ValidPassword(password) {
		return errors.New("password must be 12–256 bytes")
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	confirmation, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	defer clear(confirmation)
	if !bytes.Equal(password, confirmation) {
		return errors.New("passwords did not match")
	}
	if err := access.CreateAccount(dir, name, password, nodes); err != nil {
		return err
	}
	fmt.Printf("Created account %s\n", access.NormalizeAccount(name))
	return nil
}

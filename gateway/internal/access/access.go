package access

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	nodeID   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	deviceID = regexp.MustCompile(`^[a-f0-9]{24}$`)
)

type Device struct {
	Label           string    `json:"label"`
	Digest          string    `json:"sha256"`
	Nodes           []string  `json:"nodes"`
	ExpiresAt       time.Time `json:"expires_at"`
	AccountRevision string    `json:"account_revision,omitempty"`
	LoginID         string    `json:"login_id,omitempty"`
}

type Grant struct {
	ID string
	Device
}

func ValidNode(id string) bool   { return nodeID.MatchString(id) }
func ValidDevice(id string) bool { return deviceID.MatchString(id) }

func Path(dir, id string) string { return filepath.Join(dir, id+".json") }

// IssueDevice persists the credential's digest and returns its bearer value once.
func IssueDevice(dir string, device Device, mode os.FileMode) (string, error) {
	var idBytes [12]byte
	var secretBytes [32]byte
	rand.Read(idBytes[:])
	rand.Read(secretBytes[:])
	id, secret := hex.EncodeToString(idBytes[:]), hex.EncodeToString(secretBytes[:])
	digest := sha256.Sum256([]byte(secret))
	device.Digest = hex.EncodeToString(digest[:])
	if err := writeRecord(dir, id, device, mode); err != nil {
		return "", err
	}
	return id + "." + secret, nil
}

func writeRecord(dir, id string, record any, mode os.FileMode) error {
	file, err := os.CreateTemp(dir, ".record-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), Path(dir, id)); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func Authenticate(dir, header string, now time.Time) (Grant, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || len(token) != 89 {
		return Grant{}, false
	}
	id, secret, ok := strings.Cut(token, ".")
	if !ok || !ValidDevice(id) || len(secret) != 64 {
		return Grant{}, false
	}
	record, err := ReadDevice(dir, id)
	if err != nil || !now.Before(record.ExpiresAt) {
		return Grant{}, false
	}
	digest := sha256.Sum256([]byte(secret))
	expected, err := hex.DecodeString(record.Digest)
	if err != nil || subtle.ConstantTimeCompare(digest[:], expected) != 1 {
		return Grant{}, false
	}
	return Grant{ID: id, Device: record}, true
}

func (g Grant) Allows(node string) bool { return slices.Contains(g.Nodes, node) }

func (g Grant) StillValid(dir, node string, now time.Time) bool {
	record, err := ReadDevice(dir, g.ID)
	return err == nil && now.Before(g.ExpiresAt) && now.Before(record.ExpiresAt) &&
		record.Digest == g.Digest && slices.Contains(record.Nodes, node)
}

func ReadDevice(dir, id string) (Device, error) {
	var record Device
	if dir == "" || !ValidDevice(id) {
		return record, errors.New("invalid device registry or ID")
	}
	raw, err := os.ReadFile(Path(dir, id))
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(raw, &record)
	return record, err
}

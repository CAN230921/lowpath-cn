package access

import (
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
	Label     string    `json:"label"`
	Digest    string    `json:"sha256"`
	Nodes     []string  `json:"nodes"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Grant struct {
	ID string
	Device
}

func ValidNode(id string) bool   { return nodeID.MatchString(id) }
func ValidDevice(id string) bool { return deviceID.MatchString(id) }

func Path(dir, id string) string { return filepath.Join(dir, id+".json") }

func Authenticate(dir, header string, now time.Time) (Grant, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || len(token) != 89 {
		return Grant{}, false
	}
	id, secret, ok := strings.Cut(token, ".")
	if !ok || !ValidDevice(id) || len(secret) != 64 {
		return Grant{}, false
	}
	record, err := read(dir, id)
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
	record, err := read(dir, g.ID)
	return err == nil && now.Before(g.ExpiresAt) && now.Before(record.ExpiresAt) &&
		record.Digest == g.Digest && slices.Contains(record.Nodes, node)
}

func read(dir, id string) (Device, error) {
	var record Device
	if dir == "" {
		return record, errors.New("device registry disabled")
	}
	raw, err := os.ReadFile(Path(dir, id))
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(raw, &record)
	return record, err
}

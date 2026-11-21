package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/crypto/argon2"
)

const DefaultAccountDir = "/etc/lowpath-access/accounts"

var accountName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)

type Account struct {
	Revision string   `json:"revision"`
	Salt     string   `json:"salt"`
	Digest   string   `json:"argon2id_v1"`
	Nodes    []string `json:"nodes"`
}

func NormalizeAccount(name string) string { return strings.ToLower(strings.TrimSpace(name)) }
func ValidAccount(name string) bool       { return accountName.MatchString(name) }
func ValidPassword(password []byte) bool  { return len(password) >= 12 && len(password) <= 256 }

func passwordHash(password, salt []byte) []byte {
	return argon2.IDKey(password, salt, 3, 64*1024, 1, 32)
}

// CreateAccount atomically creates a new account, without changing an existing one.
// The registry's setgid directory supplies the gateway reader group.
func CreateAccount(dir, name string, password []byte, nodes []string) error {
	name = NormalizeAccount(name)
	if !ValidAccount(name) || !ValidPassword(password) || len(nodes) == 0 {
		return errors.New("create an account with 3–32 valid name characters, a 12–256 byte password and at least one node")
	}
	for _, node := range nodes {
		if !ValidNode(node) {
			return errors.New("invalid node ID")
		}
	}
	nodes = slices.Clone(nodes)
	slices.Sort(nodes)
	nodes = slices.Compact(nodes)
	var salt, revision [16]byte
	rand.Read(salt[:])
	rand.Read(revision[:])
	hash := passwordHash(password, salt[:])
	defer clear(hash)
	account := Account{Revision: hex.EncodeToString(revision[:]), Salt: hex.EncodeToString(salt[:]), Digest: hex.EncodeToString(hash), Nodes: nodes}
	return writeRecord(dir, name, account, 0640)
}

func ReadAccount(dir, name string) (Account, error) {
	var account Account
	if dir == "" || !ValidAccount(name) {
		return account, errors.New("invalid account")
	}
	raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return account, err
	}
	return account, json.Unmarshal(raw, &account)
}

// CheckPassword spends the same password-hash work on an unknown account.
func CheckPassword(dir, name string, password []byte) (Account, bool) {
	account, err := ReadAccount(dir, name)
	var salt [16]byte
	var expected [32]byte
	saltBytes, saltErr := hex.DecodeString(account.Salt)
	digest, digestErr := hex.DecodeString(account.Digest)
	valid := err == nil && saltErr == nil && digestErr == nil && len(saltBytes) == len(salt) && len(digest) == len(expected)
	if valid {
		copy(salt[:], saltBytes)
		copy(expected[:], digest)
	}
	actual := passwordHash(password, salt[:])
	defer clear(actual)
	return account, subtle.ConstantTimeCompare(actual, expected[:]) == 1 && valid
}

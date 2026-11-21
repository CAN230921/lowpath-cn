package managed

import (
	"net"
	"strings"
)

// CredentialStore holds only the gateway's refresh credential, never a
// password or the bearer credential sent to a relay.
type CredentialStore interface {
	Load() (string, error)
	Save(credential string) error
	Delete() error
}

// SystemCredentialStore keeps credentials separate for each HTTPS gateway.
func (n *Network) SystemCredentialStore() CredentialStore {
	port := n.origin.Port()
	if port == "" {
		port = "443"
	}
	return newCredentialStore(net.JoinHostPort(strings.ToLower(n.origin.Hostname()), port))
}

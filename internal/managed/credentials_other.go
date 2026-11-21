//go:build !darwin || !cgo

package managed

func newCredentialStore(string) CredentialStore { return nil }

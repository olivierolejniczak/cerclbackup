// Package keyring wraps the OS credential store (Windows Credential Manager,
// macOS Keychain, Linux Secret Service) so the tray app and CLI can share the
// backup password without storing it in plain text.
package keyring

import (
	gokeyring "github.com/zalando/go-keyring"
)

const (
	service = "cerclbackup"
	account = "backup-password"
)

// Get retrieves the stored backup password.
// Returns ("", err) if no password has been saved or the keyring is unavailable.
func Get() (string, error) {
	return gokeyring.Get(service, account)
}

// Set stores password in the OS keyring.
func Set(password string) error {
	return gokeyring.Set(service, account, password)
}

// Delete removes the stored password.
func Delete() error {
	return gokeyring.Delete(service, account)
}

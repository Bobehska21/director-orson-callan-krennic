// Package secrets stores and resolves credentials in the OS keychain
// (macOS Keychain, Windows Credential Manager/DPAPI, Linux Secret Service)
// via go-keyring. Secrets are referenced elsewhere only by *name*; their
// values never touch config files or disk.
package secrets

import (
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// Service is the keychain service name under which all Krennic secrets live.
const Service = "com.acme.krennic"

// Store persists a secret value under the given key name.
func Store(name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name is empty")
	}
	if err := keyring.Set(Service, name, value); err != nil {
		return fmt.Errorf("store secret %q: %w", name, err)
	}
	return nil
}

// Resolve fetches a secret value by key name.
func Resolve(name string) (string, error) {
	v, err := keyring.Get(Service, name)
	if err != nil {
		return "", fmt.Errorf("resolve secret %q (run `krennic keys set %s`): %w", name, name, err)
	}
	return v, nil
}

// Has reports whether a secret exists without returning its value.
func Has(name string) bool {
	_, err := keyring.Get(Service, name)
	return err == nil
}

// Delete removes a secret.
func Delete(name string) error {
	if err := keyring.Delete(Service, name); err != nil {
		return fmt.Errorf("delete secret %q: %w", name, err)
	}
	return nil
}

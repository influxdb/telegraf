package config

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/awnumar/memguard"

	"github.com/influxdata/telegraf/secretstore"
)

const secretStoreConfig = `
# Store secrets like credentials using a service external to telegraf
# [[secretstore]]
  ## Name of the secret-store used to reference the secrets later via @{name:secret_key} (mandatory)
  name = secretstore

  ## Define the service for storing the credentials, can be one of
  ##     file://<path>
  ##       Encrypted file at the given "path" (mandatory) for storing the secrets.
  ##     kwallet://[[application]/folder]   (default: "kwallet://telegraf")
  ##       kWallet with the given "application" ID and an optional subfolder.
  ##     os://[collection]                  (default: "os://telegraf")
  ##       OS's native secret store with "collection" being the keychain/keyring name or Windows' credential prefix
  ##     secret-service://[collection]      (default: "secret-service://telegraf")
  ##       Freedesktop secret-service implementation.
  # service = "os://telegraf"

	## Password to be used for unlocking secret-stores (e.g. encrypted files).
	## If omitted, you will be prompted for the password when starting telegraf.
	## You may use environment-variables here to allow non-interactive starts.
	# password = "$SECRETSTORE_PASSWD"
`

// secretPattern is a regex to extract references to secrets stored in a secret-store.
var secretPattern = regexp.MustCompile(`@\{(\w+:\w+)\}`)

// secretRegister contains a list of secrets for later resolving by the config.
var secretRegister = make([]*Secret, 0)

// Secret safely stores sensitive data such as a password or token
type Secret struct {
	enclave  *memguard.Enclave
	resolver func() (string, error)

	stores map[string]secretstore.SecretStore
}

// staticResolver returns static secrets that do not change over time
func (s *Secret) staticResolver() (string, error) {
	lockbuf, err := s.enclave.Open()
	if err != nil {
		return "", fmt.Errorf("opening enclave failed: %v", err)
	}

	return lockbuf.String(), nil
}

// dynamicResolver returns dynamic secrets that change over time e.g. TOTP
func (s *Secret) dynamicResolver() (string, error) {
	lockbuf, err := s.enclave.Open()
	if err != nil {
		return "", fmt.Errorf("opening enclave failed: %v", err)
	}

	return s.replace(lockbuf.String(), s.stores, false)
}

// UnmarshalTOML creates a secret from a toml value
func (s *Secret) UnmarshalTOML(b []byte) error {
	s.enclave = memguard.NewEnclave(unquote(b))
	s.resolver = s.staticResolver
	s.stores = make(map[string]secretstore.SecretStore)

	secretRegister = append(secretRegister, s)

	return nil
}

// Get return the string representation of the secret
func (s *Secret) Get() (string, error) {
	return s.resolver()
}

// Resolve all static references to secret-stores and keep the dynamic ones.
func (s *Secret) Resolve(stores map[string]secretstore.SecretStore) error {
	lockbuf, err := s.enclave.Open()
	if err != nil {
		return fmt.Errorf("opening enclave failed: %v", err)
	}

	secret, err := s.replace(lockbuf.String(), stores, true)
	if err != nil {
		return err
	}

	if lockbuf.String() != secret {
		s.enclave = memguard.NewEnclave([]byte(secret))
		lockbuf.Destroy()
	}

	return nil
}

func (s *Secret) replace(secret string, stores map[string]secretstore.SecretStore, replaceDynamic bool) (string, error) {
	replaceErrs := make([]string, 0)
	newsecret := secretPattern.ReplaceAllStringFunc(secret, func(match string) string {
		// There should _ALWAYS_ be two parts due to the regular expression match
		parts := strings.SplitN(match[2:len(match)-1], ":", 2)
		storename := parts[0]
		keyname := parts[1]

		store, found := stores[storename]
		if !found {
			replaceErrs = append(replaceErrs, fmt.Sprintf("unknown store %q for %q", storename, match))
			return match
		}

		// Do not replace secrets from a dynamic store and remember their stores
		if replaceDynamic && store.IsDynamic() {
			s.stores[storename] = store
			s.resolver = s.dynamicResolver
			return match
		}

		// Replace all secrets from static stores
		replacement, err := store.Get(keyname)
		if err != nil {
			replaceErrs = append(replaceErrs, fmt.Sprintf("getting secret %q for %q: %v", keyname, match, err))
			return match
		}
		return replacement
	})
	if len(replaceErrs) > 0 {
		return "", fmt.Errorf("replacing secrets failed: %s", strings.Join(replaceErrs, ";"))
	}

	return newsecret, nil
}

// resolveSecrets iterates over all registered secrets and resolves all resolvable references.
func (c *Config) resolveSecrets() error {
	for _, secret := range secretRegister {
		if err := secret.Resolve(c.SecretStore); err != nil {
			return err
		}
	}
	return nil
}

func unquote(b []byte) []byte {
	if bytes.HasPrefix(b, []byte("'''")) && bytes.HasSuffix(b, []byte("'''")) {
		return b[3 : len(b)-3]
	}
	if bytes.HasPrefix(b, []byte("'")) && bytes.HasSuffix(b, []byte("'")) {
		return b[1 : len(b)-1]
	}
	if bytes.HasPrefix(b, []byte("\"\"\"")) && bytes.HasSuffix(b, []byte("\"\"\"")) {
		return b[3 : len(b)-3]
	}
	if bytes.HasPrefix(b, []byte("\"")) && bytes.HasSuffix(b, []byte("\"")) {
		return b[1 : len(b)-1]
	}
	return b
}

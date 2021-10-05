package secretstore

import (
	"fmt"
	"net/url"
	"strings"
)

type storeImpl interface {
	Get(key string) (string, error)
	Set(key, value string) error
	List() ([]string, error)
}

type SecretStore struct {
	Name     string `toml:"name"`
	Service  string `toml:"service"`
	Password Secret `toml:"password"`

	store   storeImpl
	dynamic bool
}

var stores = make(map[string]*SecretStore)

// Init initializes all internals of the secret-store
func (s *SecretStore) Init() error {
	defer s.Password.Destroy()

	if s.Name == "" {
		return fmt.Errorf("name missing")
	}

	// Default
	if s.Service == "" {
		s.Service = "os://telegraf"
	}

	// Determine the service type and map it to the implementation configuration
	u, err := url.Parse(s.Service)
	if err != nil {
		return fmt.Errorf("parsing service failed: %v", err)
	}

	// Determine the additional arguments
	path := strings.TrimPrefix(strings.TrimPrefix(s.Service, u.Scheme+":"), "//")
	if path == "" {
		path = "telegraf"
	}

	switch u.Scheme {
	case "file", "kwallet", "os", "secret-service":
		passwd, err := s.Password.Get()
		if err != nil {
			return fmt.Errorf("getting password faild: %v", err)
		}

		s.store, err = NewKeyringStore(s.Name, u.Scheme, path, passwd)
		if err != nil {
			return fmt.Errorf("creating keyring store for service %q failed: %v", u.Scheme, err)
		}
	default:
		return fmt.Errorf("unknown service %q", u.Scheme)
	}

	// Register the store
	stores[s.Name] = s

	return nil
}

// Get searches for the given key and return the secret
func (s *SecretStore) Get(key string) (string, error) {
	return s.store.Get(key)
}

// Set sets the given secret for the given key
func (s *SecretStore) Set(key, value string) error {
	return s.store.Set(key, value)
}

// List lists all known secret keys
func (s *SecretStore) List() ([]string, error) {
	return s.store.List()
}

// IsDynamic returns true if the store contains secrets that change over time (e.g. TOTP)
func (s *SecretStore) IsDynamic() bool {
	return s.dynamic
}

package config

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/influxdata/telegraf"
)

// unlinkedSecrets contains the list of secrets that contain
// references not yet linked to their corresponding secret-store.
// Those secrets must later (after reading the config) be linked
// by the config to their respective secret-stores.
// Secrets containing constant strings will not be found in this
// list.
var unlinkedSecrets = make([]*Secret, 0)

// secretStorePattern is a regex to validate secret-store IDs
var secretStorePattern = regexp.MustCompile(`^\w+$`)

// secretPattern is a regex to extract references to secrets store in a secret-store
var secretPattern = regexp.MustCompile(`@\{(\w+:\w+)\}`)

// secretCandidatePattern is a regex to find secret candidates to warn users on invalid characters in references
var secretCandidatePattern = regexp.MustCompile(`@\{.+?:.+?}`)

// secretCount is the number of secrets use in Telegraf
var secretCount atomic.Int64

// selectedImpl is the configured implementation for secrets
var selectedImpl secretImpl = &protectedSecretImpl{}

// secretImpl represents an abstraction for different implementations of secrets
type secretImpl interface {
	Container(secret []byte) secretContainer
	EmptyBuffer() SecretBuffer
	Wipe(secret []byte)
}

func EnableSecretProtection() {
	selectedImpl = &protectedSecretImpl{}
}

func DisableSecretProtection() {
	selectedImpl = &unprotectedSecretImpl{}
}

// secretContainer represents an abstraction of the container holding the
// actual secret value
type secretContainer interface {
	Destroy()
	Equals(ref []byte) (bool, error)
	Buffer() (SecretBuffer, error)
	AsBuffer(secret []byte) SecretBuffer
	Replace(secret []byte)
}

// SecretBuffer allows to access the content of the secret
type SecretBuffer interface {
	// Size returns the length of the buffer content
	Size() int
	// Grow will grow the capacity of the underlying buffer to the given size
	Grow(capacity int)
	// Bytes returns the content of the buffer as bytes.
	// NOTE: The returned bytes shall NOT be accessed after destroying the
	// buffer using 'Destroy()' as the underlying the memory area might be
	// wiped and invalid.
	Bytes() []byte
	// TemporaryString returns the content of the buffer as a string.
	// NOTE: The returned String shall NOT be accessed after destroying the
	// buffer using 'Destroy()' as the underlying the memory area might be
	// wiped and invalid.
	TemporaryString() string
	// String returns a copy of the underlying buffer's content as string.
	// It is safe to use the returned value after destroying the buffer.
	String() string
	// Destroy will wipe the buffer's content and destroy the underlying
	// buffer. Do not access the buffer after destroying it.
	Destroy()
}

// Secret safely stores sensitive data such as a password or token
type Secret struct {
	// container is the implementation for holding the secret. It can be
	// protected or not depending on the concrete implementation.
	container secretContainer

	// resolvers are the functions for resolving a given secret-id (key)
	resolvers map[string]telegraf.ResolveFunc

	// unlinked contains all references in the secret that are not yet
	// linked to the corresponding secret store.
	unlinked []string

	// notempty denotes if the secret is completely empty
	notempty bool
}

// NewSecret creates a new secret from the given bytes
func NewSecret(b []byte) Secret {
	s := Secret{}
	s.init(b)
	return s
}

// UnmarshalText creates a secret from a toml value following the "string" rule.
func (s *Secret) UnmarshalText(b []byte) error {
	// Unmarshal secret from TOML and put it into protected memory
	s.init(b)

	// Keep track of secrets that contain references to secret-stores
	// for later resolving by the config.
	if len(s.unlinked) > 0 && s.notempty {
		unlinkedSecrets = append(unlinkedSecrets, s)
	}

	return nil
}

// Initialize the secret content
func (s *Secret) init(secret []byte) {
	// Keep track of the number of secrets...
	secretCount.Add(1)

	// Remember if the secret is completely empty
	s.notempty = len(secret) != 0

	// Find all secret candidates and check if they are really a valid
	// reference. Otherwise issue a warning to let the user know that there is
	// a potential issue with their secret instead of silently ignoring it.
	candidates := secretCandidatePattern.FindAllString(string(secret), -1)
	s.unlinked = make([]string, 0, len(candidates))
	for _, c := range candidates {
		if secretPattern.MatchString(c) {
			s.unlinked = append(s.unlinked, c)
		} else {
			log.Printf("W! Secret %q contains invalid character(s), only letters, digits and underscores are allowed.", c)
		}
	}
	s.resolvers = nil

	// Setup the container implementation
	s.container = selectedImpl.Container(secret)
}

// Destroy the secret content
func (s *Secret) Destroy() {
	s.resolvers = nil
	s.unlinked = nil
	s.notempty = false

	if s.container != nil {
		s.container.Destroy()
		s.container = nil

		// Keep track of the number of used secrets...
		secretCount.Add(-1)
	}
}

// Empty return if the secret is completely empty
func (s *Secret) Empty() bool {
	return !s.notempty
}

// EqualTo performs a constant-time comparison of the secret to the given reference
func (s *Secret) EqualTo(ref []byte) (bool, error) {
	if s.container == nil {
		return false, nil
	}

	if len(s.unlinked) > 0 {
		return false, fmt.Errorf("unlinked parts in secret: %v", strings.Join(s.unlinked, ";"))
	}

	return s.container.Equals(ref)
}

// Get return the string representation of the secret
func (s *Secret) Get() (SecretBuffer, error) {
	if s.container == nil {
		return selectedImpl.EmptyBuffer(), nil
	}

	if len(s.unlinked) > 0 {
		return nil, fmt.Errorf("unlinked parts in secret: %v", strings.Join(s.unlinked, ";"))
	}

	// Decrypt the secret so we can return it
	buffer, err := s.container.Buffer()
	if err != nil {
		return nil, err
	}

	// We've got a static secret so simply return the buffer
	if len(s.resolvers) == 0 {
		return buffer, nil
	}
	defer buffer.Destroy()

	replaceErrs := make([]string, 0)
	newsecret := secretPattern.ReplaceAllFunc(buffer.Bytes(), func(match []byte) []byte {
		resolver, found := s.resolvers[string(match)]
		if !found {
			replaceErrs = append(replaceErrs, fmt.Sprintf("no resolver for %q", match))
			return match
		}
		replacement, _, err := resolver()
		if err != nil {
			replaceErrs = append(replaceErrs, fmt.Sprintf("resolving %q failed: %v", match, err))
			return match
		}

		return replacement
	})
	if len(replaceErrs) > 0 {
		selectedImpl.Wipe(newsecret)
		return nil, fmt.Errorf("replacing secrets failed: %s", strings.Join(replaceErrs, ";"))
	}

	return s.container.AsBuffer(newsecret), nil
}

// Set overwrites the secret's value with a new one. Please note, the secret
// is not linked again, so only references to secret-stores can be used, e.g. by
// adding more clear-text or reordering secrets.
func (s *Secret) Set(value []byte) error {
	// Link the new value can be resolved
	secret, res, replaceErrs := resolve(value, s.resolvers)
	if len(replaceErrs) > 0 {
		return fmt.Errorf("linking new secrets failed: %s", strings.Join(replaceErrs, ";"))
	}

	// Set the new secret
	s.container.Replace(secret)
	s.resolvers = res
	s.notempty = len(value) > 0

	return nil
}

// GetUnlinked return the parts of the secret that is not yet linked to a resolver
func (s *Secret) GetUnlinked() []string {
	return s.unlinked
}

// Link used the given resolver map to link the secret parts to their
// secret-store resolvers.
func (s *Secret) Link(resolvers map[string]telegraf.ResolveFunc) error {
	// Decrypt the secret so we can return it
	if s.container == nil {
		return nil
	}
	buffer, err := s.container.Buffer()
	if err != nil {
		return err
	}
	defer buffer.Destroy()

	// Iterate through the parts and try to resolve them. For static parts
	// we directly replace them, while for dynamic ones we store the resolver.
	newsecret, res, replaceErrs := resolve(buffer.Bytes(), resolvers)
	if len(replaceErrs) > 0 {
		return fmt.Errorf("linking secrets failed: %s", strings.Join(replaceErrs, ";"))
	}
	s.resolvers = res

	// Store the secret if it has changed
	if buffer.TemporaryString() != string(newsecret) {
		s.container.Replace(newsecret)
	}

	// All linked now
	s.unlinked = nil

	return nil
}

func resolve(secret []byte, resolvers map[string]telegraf.ResolveFunc) ([]byte, map[string]telegraf.ResolveFunc, []string) {
	// Iterate through the parts and try to resolve them. For static parts
	// we directly replace them, while for dynamic ones we store the resolver.
	replaceErrs := make([]string, 0)
	remaining := make(map[string]telegraf.ResolveFunc)
	newsecret := secretPattern.ReplaceAllFunc(secret, func(match []byte) []byte {
		resolver, found := resolvers[string(match)]
		if !found {
			replaceErrs = append(replaceErrs, fmt.Sprintf("unlinked part %q", match))
			return match
		}
		replacement, dynamic, err := resolver()
		if err != nil {
			replaceErrs = append(replaceErrs, fmt.Sprintf("resolving %q failed: %v", match, err))
			return match
		}

		// Replace static parts right away
		if !dynamic {
			return replacement
		}

		// Keep the resolver for dynamic secrets
		remaining[string(match)] = resolver
		return match
	})
	return newsecret, remaining, replaceErrs
}

func splitLink(s string) (storeID, key string) {
	// There should _ALWAYS_ be two parts due to the regular expression match
	parts := strings.SplitN(s[2:len(s)-1], ":", 2)
	return parts[0], parts[1]
}

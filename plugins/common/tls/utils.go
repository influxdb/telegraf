package tls

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrCipherUnsupported = errors.New("unsupported cipher")

// InsecureCiphers returns the list of insecure ciphers among the list of given ciphers
func InsecureCiphers(ciphers []string) []string {
	var insecure []string

	for _, c := range ciphers {
		cipher := strings.ToUpper(c)
		if _, ok := tlsCipherMapInsecure[cipher]; ok {
			insecure = append(insecure, c)
		}
	}

	return insecure
}

// Ciphers returns the list of supported ciphers
func Ciphers() (secure, insecure []string) {
	for c := range tlsCipherMapSecure {
		secure = append(secure, c)
	}

	for c := range tlsCipherMapInsecure {
		insecure = append(insecure, c)
	}

	return secure, insecure
}

// ParseCiphers returns a `[]uint16` by received `[]string` key that represents ciphers from crypto/tls.
// If some of ciphers in received list doesn't exists  ParseCiphers returns nil with error
func ParseCiphers(ciphers []string) ([]uint16, error) {
	suites := make([]uint16, 0)
	added := make(map[uint16]bool, len(ciphers))
	for _, c := range ciphers {
		// Handle meta-keywords
		switch c {
		case "all":
			for _, id := range tlsCipherMapInsecure {
				if added[id] {
					continue
				}
				suites = append(suites, id)
				added[id] = true
			}
			for _, id := range tlsCipherMapSecure {
				if added[id] {
					continue
				}
				suites = append(suites, id)
				added[id] = true
			}
		case "insecure":
			for _, id := range tlsCipherMapInsecure {
				if added[id] {
					continue
				}
				suites = append(suites, id)
				added[id] = true
			}
		case "secure":
			for _, id := range tlsCipherMapSecure {
				if added[id] {
					continue
				}
				suites = append(suites, id)
				added[id] = true
			}
		default:
			cipher := strings.ToUpper(c)
			id, ok := tlsCipherMapSecure[cipher]
			if !ok {
				idInsecure, ok := tlsCipherMapInsecure[cipher]
				if !ok {
					return nil, fmt.Errorf("%q %w", cipher, ErrCipherUnsupported)
				}
				id = idInsecure
			}
			if added[id] {
				continue
			}
			suites = append(suites, id)
			added[id] = true
		}
	}

	return suites, nil
}

// ParseTLSVersion returns a `uint16` by received version string key that represents tls version from crypto/tls.
// If version isn't supported ParseTLSVersion returns 0 with error
func ParseTLSVersion(version string) (uint16, error) {
	if v, ok := tlsVersionMap[version]; ok {
		return v, nil
	}

	available := make([]string, 0, len(tlsVersionMap))
	for n := range tlsVersionMap {
		available = append(available, n)
	}
	sort.Strings(available)
	return 0, fmt.Errorf("unsupported version %q (available: %s)", version, strings.Join(available, ","))
}

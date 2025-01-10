package dcos

import (
	"context"
	"crypto/rsa"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// How long before expiration to renew token
	relogDuration = 5 * time.Minute
)

type credentials interface {
	token(ctx context.Context, client client) (string, error)
	isExpired() bool
}

type serviceAccount struct {
	accountID  string
	privateKey *rsa.PrivateKey

	auth *authToken
}

type tokenCreds struct {
	Path string
}

type nullCreds struct {
}

func (c *serviceAccount) token(ctx context.Context, client client) (string, error) {
	auth, err := client.login(ctx, c)
	if err != nil {
		return "", err
	}
	c.auth = auth
	return auth.Text, nil
}

func (c *serviceAccount) isExpired() bool {
	return c.auth.Text != "" || c.auth.Expire.Add(relogDuration).After(time.Now())
}

func (c *tokenCreds) token(_ context.Context, _ client) (string, error) {
	octets, err := os.ReadFile(c.Path)
	if err != nil {
		return "", fmt.Errorf("error reading token file %q: %w", c.Path, err)
	}
	if !utf8.Valid(octets) {
		return "", fmt.Errorf("token file does not contain utf-8 encoded text: %s", c.Path)
	}
	token := strings.TrimSpace(string(octets))
	return token, nil
}

func (*tokenCreds) isExpired() bool {
	return true
}

func (*nullCreds) token(context.Context, client) (string, error) {
	return "", nil
}

func (*nullCreds) isExpired() bool {
	return true
}

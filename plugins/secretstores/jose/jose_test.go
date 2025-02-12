package jose

import (
	"os"
	"testing"

	"github.com/influxdata/telegraf/config"
	"github.com/stretchr/testify/require"
)

func TestSampleConfig(t *testing.T) {
	plugin := &Jose{}
	require.NotEmpty(t, plugin.SampleConfig())
}

func TestInitFail(t *testing.T) {
	tests := []struct {
		name     string
		plugin   *Jose
		expected string
	}{
		{
			name:     "invalid id",
			plugin:   &Jose{},
			expected: "id missing",
		},
		{
			name: "missing path",
			plugin: &Jose{
				ID: "test",
			},
			expected: "path missing",
		},
		{
			name: "invalid password",
			plugin: &Jose{
				ID:       "test",
				Path:     os.TempDir(),
				Password: config.NewSecret([]byte("@{unresolvable:secret}")),
			},
			expected: "getting password failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.Init()
			require.ErrorContains(t, err, tt.expected)
		})
	}
}

func TestSetListGet(t *testing.T) {
	secrets := map[string]string{
		"a secret":    "I won't tell",
		"another one": "secret",
		"foo":         "bar",
	}

	// Create a temporary directory we can use to store the secrets
	testdir := t.TempDir()

	// Initialize the plugin
	plugin := &Jose{
		ID:       "test",
		Password: config.NewSecret([]byte("test")),
		Path:     testdir,
	}
	require.NoError(t, plugin.Init())

	// Store the secrets
	for k, v := range secrets {
		require.NoError(t, plugin.Set(k, v))
	}

	// Check if the secrets were actually stored
	entries, err := os.ReadDir(testdir)
	require.NoError(t, err)
	require.Len(t, entries, len(secrets))
	for _, e := range entries {
		_, found := secrets[e.Name()]
		require.True(t, found)
		require.False(t, e.IsDir())
	}

	// List the secrets
	keys, err := plugin.List()
	require.NoError(t, err)
	require.Len(t, keys, len(secrets))
	for _, k := range keys {
		_, found := secrets[k]
		require.True(t, found)
	}

	// Get the secrets
	require.Len(t, keys, len(secrets))
	for _, k := range keys {
		value, err := plugin.Get(k)
		require.NoError(t, err)
		v, found := secrets[k]
		require.True(t, found)
		require.Equal(t, v, string(value))
	}
}

func TestResolver(t *testing.T) {
	secretKey := "a secret"
	secretVal := "I won't tell"

	// Create a temporary directory we can use to store the secrets
	testdir := t.TempDir()

	// Initialize the plugin
	plugin := &Jose{
		ID:       "test",
		Password: config.NewSecret([]byte("test")),
		Path:     testdir,
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Set(secretKey, secretVal))

	// Get the resolver
	resolver, err := plugin.GetResolver(secretKey)
	require.NoError(t, err)
	require.NotNil(t, resolver)
	s, dynamic, err := resolver()
	require.NoError(t, err)
	require.False(t, dynamic)
	require.Equal(t, secretVal, string(s))
}

func TestResolverInvalid(t *testing.T) {
	secretKey := "a secret"
	secretVal := "I won't tell"

	// Create a temporary directory we can use to store the secrets
	testdir := t.TempDir()

	// Initialize the plugin
	plugin := &Jose{
		ID:       "test",
		Password: config.NewSecret([]byte("test")),
		Path:     testdir,
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Set(secretKey, secretVal))

	// Get the resolver
	resolver, err := plugin.GetResolver("foo")
	require.NoError(t, err)
	require.NotNil(t, resolver)
	_, _, err = resolver()
	require.Error(t, err)
}

func TestGetNonExistent(t *testing.T) {
	secretKey := "a secret"
	secretVal := "I won't tell"

	// Create a temporary directory we can use to store the secrets
	testdir := t.TempDir()

	// Initialize the plugin
	plugin := &Jose{
		ID:       "test",
		Password: config.NewSecret([]byte("test")),
		Path:     testdir,
	}
	require.NoError(t, plugin.Init())
	require.NoError(t, plugin.Set(secretKey, secretVal))

	// Get the resolver
	_, err := plugin.Get("foo")
	require.EqualError(t, err, "The specified item could not be found in the keyring")
}

func TestGetInvalidPassword(t *testing.T) {
	secretKey := "a secret"
	secretVal := "I won't tell"

	// Create a temporary directory we can use to store the secrets
	testdir := t.TempDir()

	// Initialize the stored secrets
	creator := &Jose{
		ID:       "test",
		Password: config.NewSecret([]byte("test")),
		Path:     testdir,
	}
	require.NoError(t, creator.Init())
	require.NoError(t, creator.Set(secretKey, secretVal))

	// Initialize the plugin with a wrong password
	// and try to access an existing secret
	plugin := &Jose{
		ID:       "test",
		Password: config.NewSecret([]byte("lala")),
		Path:     testdir,
	}
	require.NoError(t, plugin.Init())
	_, err := plugin.Get(secretKey)
	require.ErrorContains(t, err, "integrity check failed")
}

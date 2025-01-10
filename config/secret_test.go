package config

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"testing"

	"github.com/awnumar/memguard"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/secretstores"
)

func TestSecretConstantManually(t *testing.T) {
	mysecret := "a wonderful test"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	retrieved, err := s.Get()
	require.NoError(t, err)
	defer retrieved.Destroy()
	require.EqualValues(t, mysecret, retrieved.TemporaryString())
}

func TestLinking(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	resolvers := map[string]telegraf.ResolveFunc{
		"@{referenced:secret}": func() ([]byte, bool, error) {
			return []byte("resolved secret"), false, nil
		},
	}
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	require.NoError(t, s.Link(resolvers))
	retrieved, err := s.Get()
	require.NoError(t, err)
	defer retrieved.Destroy()
	require.EqualValues(t, "a resolved secret", retrieved.TemporaryString())
}

func TestLinkingResolverError(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	resolvers := map[string]telegraf.ResolveFunc{
		"@{referenced:secret}": func() ([]byte, bool, error) {
			return nil, false, errors.New("broken")
		},
	}
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	expected := `linking secrets failed: resolving "@{referenced:secret}" failed: broken`
	require.EqualError(t, s.Link(resolvers), expected)
}

func TestGettingUnlinked(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	_, err := s.Get()
	require.ErrorContains(t, err, "unlinked parts in secret")
}

func TestGettingMissingResolver(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	s.unlinked = make([]string, 0)
	s.resolvers = map[string]telegraf.ResolveFunc{
		"@{a:dummy}": func() ([]byte, bool, error) {
			return nil, false, nil
		},
	}
	_, err := s.Get()
	expected := `replacing secrets failed: no resolver for "@{referenced:secret}"`
	require.EqualError(t, err, expected)
}

func TestGettingResolverError(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	s.unlinked = make([]string, 0)
	s.resolvers = map[string]telegraf.ResolveFunc{
		"@{referenced:secret}": func() ([]byte, bool, error) {
			return nil, false, errors.New("broken")
		},
	}
	_, err := s.Get()
	expected := `replacing secrets failed: resolving "@{referenced:secret}" failed: broken`
	require.EqualError(t, err, expected)
}

func TestUninitializedEnclave(t *testing.T) {
	s := Secret{}
	defer s.Destroy()
	require.NoError(t, s.Link(map[string]telegraf.ResolveFunc{}))
	retrieved, err := s.Get()
	require.NoError(t, err)
	defer retrieved.Destroy()
	require.Empty(t, retrieved.Bytes())
}

func TestEnclaveOpenError(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	memguard.Purge()
	err := s.Link(map[string]telegraf.ResolveFunc{})
	require.ErrorContains(t, err, "opening enclave failed")

	s.unlinked = make([]string, 0)
	_, err = s.Get()
	require.ErrorContains(t, err, "opening enclave failed")
}

func TestMissingResolver(t *testing.T) {
	mysecret := "a @{referenced:secret}"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()
	err := s.Link(map[string]telegraf.ResolveFunc{})
	require.ErrorContains(t, err, "linking secrets failed: unlinked part")
}

func TestSecretConstant(t *testing.T) {
	tests := []struct {
		name     string
		cfg      []byte
		expected string
	}{
		{
			name: "simple string",
			cfg: []byte(`
				[[inputs.mockup]]
				  secret = "a secret"
			`),
			expected: "a secret",
		},
		{
			name: "mail address",
			cfg: []byte(`
				[[inputs.mockup]]
				  secret = "someone@mock.org"
			`),
			expected: "someone@mock.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewConfig()
			require.NoError(t, c.LoadConfigData(tt.cfg))
			require.Len(t, c.Inputs, 1)

			// Create a mockup secretstore
			store := &MockupSecretStore{
				Secrets: map[string][]byte{"mock": []byte("fail")},
			}
			require.NoError(t, store.Init())
			c.SecretStores["mock"] = store
			require.NoError(t, c.LinkSecrets())

			plugin := c.Inputs[0].Input.(*MockupSecretPlugin)
			secret, err := plugin.Secret.Get()
			require.NoError(t, err)
			defer secret.Destroy()

			require.EqualValues(t, tt.expected, secret.TemporaryString())
		})
	}
}

func TestSecretUnquote(t *testing.T) {
	tests := []struct {
		name string
		cfg  []byte
	}{
		{
			name: "single quotes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = 'a secret'
					expected = 'a secret'
			`),
		},
		{
			name: "double quotes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = "a secret"
					expected = "a secret"
			`),
		},
		{
			name: "triple single quotes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = '''a secret'''
					expected = '''a secret'''
			`),
		},
		{
			name: "triple double quotes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = """a secret"""
					expected = """a secret"""
			`),
		},
		{
			name: "escaped double quotes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = "\"a secret\""
					expected = "\"a secret\""
			`),
		},
		{
			name: "mix double-single quotes (single)",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = "'a secret'"
					expected = "'a secret'"
			`),
		},
		{
			name: "mix single-double quotes (single)",
			cfg: []byte(`
				[[inputs.mockup]]
				secret = '"a secret"'
				expected = '"a secret"'
			`),
		},
		{
			name: "mix double-single quotes (triple-single)",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = """'a secret'"""
					expected = """'a secret'"""
			`),
		},
		{
			name: "mix single-double quotes (triple-single)",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = '''"a secret"'''
					expected = '''"a secret"'''
			`),
		},
		{
			name: "mix double-single quotes (triple)",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = """'''a secret'''"""
					expected = """'''a secret'''"""
			`),
		},
		{
			name: "mix single-double quotes (triple)",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = '''"""a secret"""'''
					expected = '''"""a secret"""'''
			`),
		},
		{
			name: "single quotes with backslashes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = 'Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;'
					expected = 'Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;'
			`),
		},
		{
			name: "double quotes with backslashes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = "Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;"
					expected = "Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;"
			`),
		},
		{
			name: "triple single quotes with backslashes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = '''Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;'''
					expected = '''Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;'''
			`),
		},
		{
			name: "triple double quotes with backslashes",
			cfg: []byte(`
				[[inputs.mockup]]
					secret = """Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;"""
					expected = """Server=SQLTELEGRAF\\SQL2022;app name=telegraf;log=1;"""
			`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewConfig()
			require.NoError(t, c.LoadConfigData(tt.cfg))
			require.Len(t, c.Inputs, 1)

			// Create a mockup secretstore
			store := &MockupSecretStore{
				Secrets: map[string][]byte{},
			}
			require.NoError(t, store.Init())
			c.SecretStores["mock"] = store
			require.NoError(t, c.LinkSecrets())

			plugin := c.Inputs[0].Input.(*MockupSecretPlugin)
			secret, err := plugin.Secret.Get()
			require.NoError(t, err)
			defer secret.Destroy()

			require.EqualValues(t, plugin.Expected, secret.TemporaryString())
		})
	}
}

func TestSecretEnvironmentVariable(t *testing.T) {
	cfg := []byte(`
[[inputs.mockup]]
	secret = "$SOME_ENV_SECRET"
`)
	t.Setenv("SOME_ENV_SECRET", "an env secret")

	c := NewConfig()
	err := c.LoadConfigData(cfg)
	require.NoError(t, err)
	require.Len(t, c.Inputs, 1)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{},
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	plugin := c.Inputs[0].Input.(*MockupSecretPlugin)
	secret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer secret.Destroy()

	require.EqualValues(t, "an env secret", secret.TemporaryString())
}

func TestSecretCount(t *testing.T) {
	secretCount.Store(0)
	cfg := []byte(`
[[inputs.mockup]]

[[inputs.mockup]]
  secret = "a secret"

[[inputs.mockup]]
  secret = "another secret"
`)

	c := NewConfig()
	require.NoError(t, c.LoadConfigData(cfg))
	require.Len(t, c.Inputs, 3)
	require.Equal(t, int64(2), secretCount.Load())

	// Remove all secrets and check
	for _, ri := range c.Inputs {
		input := ri.Input.(*MockupSecretPlugin)
		input.Secret.Destroy()
	}
	require.Equal(t, int64(0), secretCount.Load())
}

func TestSecretStoreStatic(t *testing.T) {
	cfg := []byte(
		`
[[inputs.mockup]]
	secret = "@{mock:secret1}"
[[inputs.mockup]]
	secret = "@{mock:secret2}"
[[inputs.mockup]]
	secret = "@{mock:a_strange_secret}"
[[inputs.mockup]]
	secret = "@{mock:a_weird_secret}"
`)

	c := NewConfig()
	err := c.LoadConfigData(cfg)
	require.NoError(t, err)
	require.Len(t, c.Inputs, 4)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{
			"secret1":          []byte("Ood Bnar"),
			"secret2":          []byte("Thon"),
			"a_strange_secret": []byte("Obi-Wan Kenobi"),
			"a_weird_secret":   []byte("Arca Jeth"),
		},
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	expected := []string{"Ood Bnar", "Thon", "Obi-Wan Kenobi", "Arca Jeth"}
	for i, input := range c.Inputs {
		plugin := input.Input.(*MockupSecretPlugin)
		secret, err := plugin.Secret.Get()
		require.NoError(t, err)
		require.EqualValues(t, expected[i], secret.TemporaryString())
		secret.Destroy()
	}
}

func TestSecretStoreInvalidKeys(t *testing.T) {
	cfg := []byte(
		`
[[inputs.mockup]]
	secret = "@{mock:}"
[[inputs.mockup]]
	secret = "@{mock:wild?%go}"
[[inputs.mockup]]
	secret = "@{mock:a-strange-secret}"
[[inputs.mockup]]
	secret = "@{mock:a weird secret}"
`)

	c := NewConfig()
	err := c.LoadConfigData(cfg)
	require.NoError(t, err)
	require.Len(t, c.Inputs, 4)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{
			"":                 []byte("Ood Bnar"),
			"wild?%go":         []byte("Thon"),
			"a-strange-secret": []byte("Obi-Wan Kenobi"),
			"a weird secret":   []byte("Arca Jeth"),
		},
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	expected := []string{
		"@{mock:}",
		"@{mock:wild?%go}",
		"@{mock:a-strange-secret}",
		"@{mock:a weird secret}",
	}
	for i, input := range c.Inputs {
		plugin := input.Input.(*MockupSecretPlugin)
		secret, err := plugin.Secret.Get()
		require.NoError(t, err)
		require.EqualValues(t, expected[i], secret.TemporaryString())
		secret.Destroy()
	}
}

func TestSecretStoreDeclarationMissingID(t *testing.T) {
	defer func() { unlinkedSecrets = make([]*Secret, 0) }()

	cfg := []byte(`[[secretstores.mockup]]`)

	c := NewConfig()
	err := c.LoadConfigData(cfg)
	require.ErrorContains(t, err, `error parsing mockup, "mockup" secret-store without ID`)
}

func TestSecretStoreDeclarationInvalidID(t *testing.T) {
	defer func() { unlinkedSecrets = make([]*Secret, 0) }()

	invalidIDs := []string{"foo.bar", "dummy-123", "test!", "wohoo+"}
	tmpl := `
  [[secretstores.mockup]]
    id = %q
`
	for _, id := range invalidIDs {
		t.Run(id, func(t *testing.T) {
			cfg := []byte(fmt.Sprintf(tmpl, id))
			c := NewConfig()
			err := c.LoadConfigData(cfg)
			require.ErrorContains(t, err, `error parsing mockup, invalid secret-store ID`)
		})
	}
}

func TestSecretStoreDeclarationValidID(t *testing.T) {
	defer func() { unlinkedSecrets = make([]*Secret, 0) }()

	validIDs := []string{"foobar", "dummy123", "test_id", "W0Hoo_lala123"}
	tmpl := `
  [[secretstores.mockup]]
    id = %q
`
	for _, id := range validIDs {
		t.Run(id, func(t *testing.T) {
			cfg := []byte(fmt.Sprintf(tmpl, id))
			c := NewConfig()
			err := c.LoadConfigData(cfg)
			require.NoError(t, err)
		})
	}
}

type SecretImplTestSuite struct {
	suite.Suite
	protected bool
}

func (tsuite *SecretImplTestSuite) SetupSuite() {
	if tsuite.protected {
		EnableSecretProtection()
	} else {
		DisableSecretProtection()
	}
}

func (*SecretImplTestSuite) TearDownSuite() {
	EnableSecretProtection()
}

func (*SecretImplTestSuite) TearDownTest() {
	unlinkedSecrets = make([]*Secret, 0)
}

func (tsuite *SecretImplTestSuite) TestSecretEqualTo() {
	t := tsuite.T()
	mysecret := "a wonderful test"
	s := NewSecret([]byte(mysecret))
	defer s.Destroy()

	equal, err := s.EqualTo([]byte(mysecret))
	require.NoError(t, err)
	require.True(t, equal)

	equal, err = s.EqualTo([]byte("some random text"))
	require.NoError(t, err)
	require.False(t, equal)
}

func (tsuite *SecretImplTestSuite) TestSecretStoreInvalidReference() {
	t := tsuite.T()

	cfg := []byte(
		`
[[inputs.mockup]]
	secret = "@{mock:test}"
`)

	c := NewConfig()
	require.NoError(t, c.LoadConfigData(cfg))
	require.Len(t, c.Inputs, 1)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{"test": []byte("Arca Jeth")},
	}
	require.NoError(t, store.Init())
	c.SecretStores["foo"] = store
	err := c.LinkSecrets()
	require.EqualError(t, err, `unknown secret-store for "@{mock:test}"`)

	for _, input := range c.Inputs {
		plugin := input.Input.(*MockupSecretPlugin)
		secret, err := plugin.Secret.Get()
		require.EqualError(t, err, `unlinked parts in secret: @{mock:test}`)
		require.Empty(t, secret)
	}
}

func (tsuite *SecretImplTestSuite) TestSecretStoreStaticChanging() {
	t := tsuite.T()

	cfg := []byte(
		`
[[inputs.mockup]]
	secret = "@{mock:secret}"
`)

	c := NewConfig()
	err := c.LoadConfigData(cfg)
	require.NoError(t, err)
	require.Len(t, c.Inputs, 1)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{"secret": []byte("Ood Bnar")},
		Dynamic: false,
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	sequence := []string{"Ood Bnar", "Thon", "Obi-Wan Kenobi", "Arca Jeth"}
	plugin := c.Inputs[0].Input.(*MockupSecretPlugin)
	secret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer secret.Destroy()

	require.EqualValues(t, "Ood Bnar", secret.TemporaryString())

	for _, v := range sequence {
		store.Secrets["secret"] = []byte(v)
		secret, err := plugin.Secret.Get()
		require.NoError(t, err)

		// The secret should not change as the store is marked non-dyamic!
		require.EqualValues(t, "Ood Bnar", secret.TemporaryString())
		secret.Destroy()
	}
}

func (tsuite *SecretImplTestSuite) TestSecretStoreDynamic() {
	t := tsuite.T()

	cfg := []byte(
		`
[[inputs.mockup]]
	secret = "@{mock:secret}"
`)

	c := NewConfig()
	err := c.LoadConfigData(cfg)
	require.NoError(t, err)
	require.Len(t, c.Inputs, 1)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{"secret": []byte("Ood Bnar")},
		Dynamic: true,
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	sequence := []string{"Ood Bnar", "Thon", "Obi-Wan Kenobi", "Arca Jeth"}
	plugin := c.Inputs[0].Input.(*MockupSecretPlugin)
	for _, v := range sequence {
		store.Secrets["secret"] = []byte(v)
		secret, err := plugin.Secret.Get()
		require.NoError(t, err)

		// The secret should not change as the store is marked non-dynamic!
		require.EqualValues(t, v, secret.TemporaryString())
		secret.Destroy()
	}
}

func (tsuite *SecretImplTestSuite) TestSecretSet() {
	t := tsuite.T()

	cfg := []byte(`
      [[inputs.mockup]]
	    secret = "a secret"
	`)
	c := NewConfig()
	require.NoError(t, c.LoadConfigData(cfg))
	require.Len(t, c.Inputs, 1)
	require.NoError(t, c.LinkSecrets())

	plugin := c.Inputs[0].Input.(*MockupSecretPlugin)

	secret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer secret.Destroy()
	require.EqualValues(t, "a secret", secret.TemporaryString())

	require.NoError(t, plugin.Secret.Set([]byte("another secret")))
	newsecret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer newsecret.Destroy()
	require.EqualValues(t, "another secret", newsecret.TemporaryString())
}

func (tsuite *SecretImplTestSuite) TestSecretSetResolve() {
	t := tsuite.T()
	cfg := []byte(`
      [[inputs.mockup]]
	    secret = "@{mock:secret}"
	`)
	c := NewConfig()
	require.NoError(t, c.LoadConfigData(cfg))
	require.Len(t, c.Inputs, 1)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{"secret": []byte("Ood Bnar")},
		Dynamic: true,
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	plugin := c.Inputs[0].Input.(*MockupSecretPlugin)

	secret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer secret.Destroy()
	require.EqualValues(t, "Ood Bnar", secret.TemporaryString())

	require.NoError(t, plugin.Secret.Set([]byte("@{mock:secret} is cool")))
	newsecret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer newsecret.Destroy()
	require.EqualValues(t, "Ood Bnar is cool", newsecret.TemporaryString())
}

func (tsuite *SecretImplTestSuite) TestSecretSetResolveInvalid() {
	t := tsuite.T()

	cfg := []byte(`
      [[inputs.mockup]]
	    secret = "@{mock:secret}"
	`)
	c := NewConfig()
	require.NoError(t, c.LoadConfigData(cfg))
	require.Len(t, c.Inputs, 1)

	// Create a mockup secretstore
	store := &MockupSecretStore{
		Secrets: map[string][]byte{"secret": []byte("Ood Bnar")},
		Dynamic: true,
	}
	require.NoError(t, store.Init())
	c.SecretStores["mock"] = store
	require.NoError(t, c.LinkSecrets())

	plugin := c.Inputs[0].Input.(*MockupSecretPlugin)

	secret, err := plugin.Secret.Get()
	require.NoError(t, err)
	defer secret.Destroy()
	require.EqualValues(t, "Ood Bnar", secret.TemporaryString())

	err = plugin.Secret.Set([]byte("@{mock:another_secret}"))
	require.ErrorContains(t, err, `linking new secrets failed: unlinked part "@{mock:another_secret}"`)
}

func (tsuite *SecretImplTestSuite) TestSecretInvalidWarn() {
	t := tsuite.T()

	// Intercept the log output
	var buf bytes.Buffer
	backup := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(backup)

	cfg := []byte(`
      [[inputs.mockup]]
	    secret = "server=a user=@{mock:secret-with-invalid-chars} pass=@{mock:secret_pass}"
	`)
	c := NewConfig()
	require.NoError(t, c.LoadConfigData(cfg))
	require.Len(t, c.Inputs, 1)

	require.Contains(t, buf.String(), `W! Secret "@{mock:secret-with-invalid-chars}" contains invalid character(s)`)
	require.NotContains(t, buf.String(), "@{mock:secret_pass}")
}

func TestSecretImplUnprotected(t *testing.T) {
	impl := &unprotectedSecretImpl{}
	container := impl.Container([]byte("foobar"))
	require.NotNil(t, container)
	c, ok := container.(*unprotectedSecretContainer)
	require.True(t, ok)
	require.Equal(t, "foobar", string(c.buf.content))
	buf, err := container.Buffer()
	require.NoError(t, err)
	require.NotNil(t, buf)
	require.Equal(t, []byte("foobar"), buf.Bytes())
	require.Equal(t, "foobar", buf.TemporaryString())
	require.Equal(t, "foobar", buf.String())
}

func TestSecretImplTestSuiteUnprotected(t *testing.T) {
	suite.Run(t, &SecretImplTestSuite{protected: false})
}

func TestSecretImplTestSuiteProtected(t *testing.T) {
	suite.Run(t, &SecretImplTestSuite{protected: true})
}

// Mockup (input) plugin for testing to avoid cyclic dependencies
type MockupSecretPlugin struct {
	Secret   Secret `toml:"secret"`
	Expected string `toml:"expected"`
}

func (*MockupSecretPlugin) SampleConfig() string                { return "Mockup test secret plugin" }
func (*MockupSecretPlugin) Gather(_ telegraf.Accumulator) error { return nil }

type MockupSecretStore struct {
	Secrets map[string][]byte
	Dynamic bool
}

func (s *MockupSecretStore) Init() error {
	return nil
}
func (*MockupSecretStore) SampleConfig() string {
	return "Mockup test secret plugin"
}

func (s *MockupSecretStore) Get(key string) ([]byte, error) {
	v, found := s.Secrets[key]
	if !found {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (s *MockupSecretStore) Set(key, value string) error {
	s.Secrets[key] = []byte(value)
	return nil
}

func (s *MockupSecretStore) List() ([]string, error) {
	keys := make([]string, 0, len(s.Secrets))
	for k := range s.Secrets {
		keys = append(keys, k)
	}
	return keys, nil
}
func (s *MockupSecretStore) GetResolver(key string) (telegraf.ResolveFunc, error) {
	return func() ([]byte, bool, error) {
		v, err := s.Get(key)
		return v, s.Dynamic, err
	}, nil
}

// Register the mockup plugin on loading
func init() {
	// Register the mockup input plugin for the required names
	inputs.Add("mockup", func() telegraf.Input { return &MockupSecretPlugin{} })
	secretstores.Add("mockup", func(string) telegraf.SecretStore {
		return &MockupSecretStore{}
	})
}

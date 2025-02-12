package x509_cert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pion/dtls/v2"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/testutil"
)

var pki = testutil.NewPKI("../../../testutil/pki")

// Make sure X509Cert implements telegraf.Input
var _ telegraf.Input = &X509Cert{}

func TestGatherRemoteIntegration(t *testing.T) {
	t.Skip("Skipping network-dependent test due to race condition when test-all")

	tmpfile, err := os.CreateTemp(t.TempDir(), "example")
	require.NoError(t, err)

	_, err = tmpfile.WriteString(pki.ReadServerCert())
	require.NoError(t, err)

	tests := []struct {
		name    string
		server  string
		timeout time.Duration
		close   bool
		unset   bool
		noshake bool
		error   bool
	}{
		{name: "wrong port", server: ":99999", error: true},
		{name: "no server", timeout: 5},
		{name: "successful https", server: "https://example.org:443", timeout: 5},
		{name: "successful file", server: "file://" + filepath.ToSlash(tmpfile.Name()), timeout: 5},
		{name: "unsupported scheme", server: "foo://", timeout: 5, error: true},
		{name: "no certificate", timeout: 5, unset: true, error: true},
		{name: "closed connection", close: true, error: true},
		{name: "no handshake", timeout: 5, noshake: true, error: true},
	}

	pair, err := tls.X509KeyPair([]byte(pki.ReadServerCert()), []byte(pki.ReadServerKey()))
	require.NoError(t, err)

	cfg := &tls.Config{
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{pair},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.unset {
				cfg.Certificates = nil
				cfg.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					return nil, nil
				}
			}

			ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
			require.NoError(t, err)
			defer ln.Close()

			go func() {
				sconn, err := ln.Accept()
				if err != nil {
					t.Error(err)
					return
				}

				if test.close {
					sconn.Close()
				}

				serverConfig := cfg.Clone()
				srv := tls.Server(sconn, serverConfig)
				if test.noshake {
					srv.Close()
				}

				if err = srv.Handshake(); err != nil {
					t.Error(err)
					return
				}
			}()

			if test.server == "" {
				test.server = "tcp://" + ln.Addr().String()
			}

			sc := X509Cert{
				Sources: []string{test.server},
				Timeout: config.Duration(test.timeout),
				Log:     testutil.Logger{},
			}
			require.NoError(t, sc.Init())

			sc.InsecureSkipVerify = true
			testErr := false

			acc := testutil.Accumulator{}
			err = sc.Gather(&acc)
			if len(acc.Errors) > 0 {
				testErr = true
			}

			if testErr != test.error {
				t.Errorf("%s", err)
			}
		})
	}
}

func TestGatherLocal(t *testing.T) {
	wrongCert := fmt.Sprintf("-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----\n", base64.StdEncoding.EncodeToString([]byte("test")))

	tests := []struct {
		name    string
		mode    os.FileMode
		content string
		error   bool
	}{
		{name: "permission denied", mode: 0001, error: true},
		{name: "not a certificate", mode: 0640, content: "test", error: true},
		{name: "wrong certificate", mode: 0640, content: wrongCert, error: true},
		{name: "correct certificate", mode: 0640, content: pki.ReadServerCert()},
		{name: "correct client certificate", mode: 0640, content: pki.ReadClientCert()},
		{name: "correct certificate and extra trailing space", mode: 0640, content: pki.ReadServerCert() + " "},
		{name: "correct certificate and extra leading space", mode: 0640, content: " " + pki.ReadServerCert()},
		{name: "correct multiple certificates", mode: 0640, content: pki.ReadServerCert() + pki.ReadCACert()},
		{name: "correct multiple certificates and key", mode: 0640, content: pki.ReadServerCert() + pki.ReadCACert() + pki.ReadServerKey()},
		{name: "correct certificate and wrong certificate", mode: 0640, content: pki.ReadServerCert() + "\n" + wrongCert, error: true},
		{name: "correct certificate and not a certificate", mode: 0640, content: pki.ReadServerCert() + "\ntest", error: true},
		{name: "correct multiple certificates and extra trailing space", mode: 0640, content: pki.ReadServerCert() + pki.ReadServerCert() + " "},
		{name: "correct multiple certificates and extra leading space", mode: 0640, content: " " + pki.ReadServerCert() + pki.ReadServerCert()},
		{name: "correct multiple certificates and extra middle space", mode: 0640, content: pki.ReadServerCert() + " " + pki.ReadServerCert()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "x509_cert")
			require.NoError(t, err)

			_, err = f.WriteString(test.content)
			require.NoError(t, err)

			if runtime.GOOS != "windows" {
				require.NoError(t, f.Chmod(test.mode))
			}

			require.NoError(t, f.Close())

			sc := X509Cert{
				Sources: []string{f.Name()},
				Log:     testutil.Logger{},
			}
			require.NoError(t, sc.Init())

			acc := testutil.Accumulator{}
			err = sc.Gather(&acc)

			if (len(acc.Errors) > 0) != test.error {
				t.Errorf("%s", err)
			}
		})
	}
}

func TestTags(t *testing.T) {
	cert := fmt.Sprintf("%s\n%s", pki.ReadServerCert(), pki.ReadCACert())

	f, err := os.CreateTemp(t.TempDir(), "x509_cert")
	require.NoError(t, err)

	_, err = f.WriteString(cert)
	require.NoError(t, err)

	require.NoError(t, f.Close())

	defer os.Remove(f.Name())

	sc := X509Cert{
		Sources: []string{f.Name()},
		Log:     testutil.Logger{},
	}
	require.NoError(t, sc.Init())

	acc := testutil.Accumulator{}
	require.NoError(t, sc.Gather(&acc))

	require.True(t, acc.HasMeasurement("x509_cert"))

	require.True(t, acc.HasTag("x509_cert", "common_name"))
	require.Equal(t, "localhost", acc.TagValue("x509_cert", "common_name"))

	require.True(t, acc.HasTag("x509_cert", "signature_algorithm"))
	require.Equal(t, "SHA256-RSA", acc.TagValue("x509_cert", "signature_algorithm"))

	require.True(t, acc.HasTag("x509_cert", "public_key_algorithm"))
	require.Equal(t, "RSA", acc.TagValue("x509_cert", "public_key_algorithm"))

	require.True(t, acc.HasTag("x509_cert", "issuer_common_name"))
	require.Equal(t, "Telegraf Test CA", acc.TagValue("x509_cert", "issuer_common_name"))

	require.True(t, acc.HasTag("x509_cert", "san"))
	require.Equal(t, "localhost,127.0.0.1", acc.TagValue("x509_cert", "san"))

	require.True(t, acc.HasTag("x509_cert", "serial_number"))
	serialNumber := new(big.Int)
	_, validSerialNumber := serialNumber.SetString(acc.TagValue("x509_cert", "serial_number"), 16)
	require.Truef(t, validSerialNumber, "Expected a valid Hex serial number but got %s", acc.TagValue("x509_cert", "serial_number"))
	require.Equal(t, big.NewInt(1), serialNumber)

	// expect root/intermediate certs (more than one cert)
	require.Greater(t, acc.NMetrics(), uint64(1))
}

func TestGatherExcludeRootCerts(t *testing.T) {
	cert := fmt.Sprintf("%s\n%s", pki.ReadServerCert(), pki.ReadCACert())

	f, err := os.CreateTemp(t.TempDir(), "x509_cert")
	require.NoError(t, err)

	_, err = f.WriteString(cert)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	sc := X509Cert{
		Sources:          []string{f.Name()},
		ExcludeRootCerts: true,
		Log:              testutil.Logger{},
	}
	require.NoError(t, sc.Init())

	acc := testutil.Accumulator{}
	require.NoError(t, sc.Gather(&acc))

	require.True(t, acc.HasMeasurement("x509_cert"))
	require.Equal(t, uint64(1), acc.NMetrics())
}

func TestGatherChain(t *testing.T) {
	cert := fmt.Sprintf("%s\n%s", pki.ReadServerCert(), pki.ReadCACert())

	tests := []struct {
		name    string
		content string
		error   bool
	}{
		{name: "chain certificate", content: cert},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "x509_cert")
			require.NoError(t, err)

			_, err = f.WriteString(test.content)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			sc := X509Cert{
				Sources: []string{f.Name()},
				Log:     testutil.Logger{},
			}
			require.NoError(t, sc.Init())

			acc := testutil.Accumulator{}
			err = sc.Gather(&acc)
			if (err != nil) != test.error {
				t.Errorf("%s", err)
			}
		})
	}
}

func TestGatherUDPCertIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	pair, err := tls.X509KeyPair([]byte(pki.ReadServerCert()), []byte(pki.ReadServerKey()))
	require.NoError(t, err)

	cfg := &dtls.Config{
		Certificates: []tls.Certificate{pair},
	}

	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
	listener, err := dtls.Listen("udp", addr, cfg)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		if _, err := listener.Accept(); err != nil {
			t.Error(err)
		}
	}()

	m := &X509Cert{
		Sources: []string{"udp://" + listener.Addr().String()},
		Log:     testutil.Logger{},
	}
	require.NoError(t, m.Init())

	var acc testutil.Accumulator
	require.NoError(t, m.Gather(&acc))

	require.Empty(t, acc.Errors)
	require.True(t, acc.HasMeasurement("x509_cert"))
	require.True(t, acc.HasTag("x509_cert", "ocsp_stapled"))
}

func TestGatherTCPCert(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	m := &X509Cert{
		Sources: []string{ts.URL},
		Log:     testutil.Logger{},
	}
	require.NoError(t, m.Init())

	var acc testutil.Accumulator
	require.NoError(t, m.Gather(&acc))

	require.Empty(t, acc.Errors)
	require.True(t, acc.HasMeasurement("x509_cert"))
}

func TestGatherCertIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	m := &X509Cert{
		Sources: []string{"https://www.influxdata.com:443"},
		Log:     testutil.Logger{},
	}
	require.NoError(t, m.Init())

	var acc testutil.Accumulator
	require.NoError(t, m.Gather(&acc))

	require.True(t, acc.HasMeasurement("x509_cert"))
	require.True(t, acc.HasTag("x509_cert", "ocsp_stapled"))
}

func TestGatherCertMustNotTimeoutIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	duration := time.Duration(15) * time.Second
	m := &X509Cert{
		Sources: []string{"https://www.influxdata.com:443"},
		Timeout: config.Duration(duration),
		Log:     testutil.Logger{},
	}
	require.NoError(t, m.Init())

	var acc testutil.Accumulator
	require.NoError(t, m.Gather(&acc))
	require.Empty(t, acc.Errors)
	require.True(t, acc.HasMeasurement("x509_cert"))
	require.True(t, acc.HasTag("x509_cert", "ocsp_stapled"))
}

func TestSourcesToURLs(t *testing.T) {
	m := &X509Cert{
		Sources: []string{
			"https://www.influxdata.com:443",
			"tcp://influxdata.com:443",
			"smtp://influxdata.com:25",
			"file:///dummy_test_path_file.pem",
			"file:///windows/temp/test.pem",
			`file://C:\windows\temp\test.pem`,
			`file:///C:/windows/temp/test.pem`,
			"/tmp/dummy_test_path_glob*.pem",
		},
		Log: testutil.Logger{},
	}
	require.NoError(t, m.Init())

	expected := []string{
		"https://www.influxdata.com:443",
		"tcp://influxdata.com:443",
		"smtp://influxdata.com:25",
	}

	expectedPaths := []string{
		"/dummy_test_path_file.pem",
		"/windows/temp/test.pem",
		"C:\\windows\\temp\\test.pem",
		"C:/windows/temp/test.pem",
	}

	for _, p := range expectedPaths {
		expected = append(expected, filepath.FromSlash(p))
	}

	actual := make([]string, 0, len(m.globpaths)+len(m.locations))
	for _, p := range m.globpaths {
		actual = append(actual, p.GetRoots()...)
	}
	for _, p := range m.locations {
		actual = append(actual, p.String())
	}
	require.Len(t, m.globpaths, 5)
	require.Len(t, m.locations, 3)
	require.ElementsMatch(t, expected, actual)
}

func TestServerName(t *testing.T) {
	tests := []struct {
		name     string
		fromTLS  string
		fromCfg  string
		url      string
		expected string
		err      bool
	}{
		{name: "in cfg", fromCfg: "example.com", url: "https://other.example.com", expected: "example.com"},
		{name: "in tls", fromTLS: "example.com", url: "https://other.example.com", expected: "example.com"},
		{name: "from URL", url: "https://other.example.com", expected: "other.example.com"},
		{name: "errors", fromCfg: "otherex.com", fromTLS: "example.com", url: "https://other.example.com", err: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sc := &X509Cert{
				Sources:      []string{test.url},
				ServerName:   test.fromCfg,
				ClientConfig: common_tls.ClientConfig{ServerName: test.fromTLS},
				Log:          testutil.Logger{},
			}
			err := sc.Init()
			if test.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			u, err := url.Parse(test.url)
			require.NoError(t, err)
			require.Equal(t, test.expected, sc.serverName(u))
		})
	}
}

func TestCertificateSerialNumberRetainsLeadingZeroes(t *testing.T) {
	bi := &big.Int{}
	bi.SetString("123456789abcdef", 16)

	plugin := &X509Cert{}
	certificate := &x509.Certificate{
		SerialNumber: bi,
	}

	require.Equal(t, "123456789abcdef", plugin.getSerialNumberString(certificate))
	plugin.PadSerial = true
	require.Equal(t, "0123456789abcdef", plugin.getSerialNumberString(certificate))
}

// Bases on code from
// https://medium.com/@shaneutt/create-sign-x509-certificates-in-golang-8ac4ae49f903
func TestClassification(t *testing.T) {
	start := time.Now()
	end := time.Now().AddDate(0, 0, 1)
	tmpDir := t.TempDir()

	// Create the CA certificate
	caPriv, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(t, err)

	ca := &x509.Certificate{
		SerialNumber: big.NewInt(342350),
		Subject: pkix.Name{
			Organization: []string{"Testing Inc."},
			Country:      []string{"US"},
			CommonName:   "Root CA",
		},
		NotBefore:             start,
		NotAfter:              end,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPriv.PublicKey, caPriv)
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caBytes})

	// Write CA cert
	f, err := os.Create(filepath.Join(tmpDir, "ca.pem"))
	require.NoError(t, err)
	_, err = f.Write(caPEM)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Create an intermediate certificate
	intermediatePriv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	intermediate := &x509.Certificate{
		SerialNumber: big.NewInt(342351),
		Subject: pkix.Name{
			Organization: []string{"Testing Inc."},
			Country:      []string{"US"},
			CommonName:   "Intermediate CA",
		},
		NotBefore:             start,
		NotAfter:              end,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	intermediateBytes, err := x509.CreateCertificate(rand.Reader, intermediate, ca, &intermediatePriv.PublicKey, caPriv)
	require.NoError(t, err)
	intermediatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: intermediateBytes})

	// Create a leaf certificate
	leafPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(342352),
		Subject: pkix.Name{
			Organization: []string{"Testing Inc."},
			Country:      []string{"US"},
			CommonName:   "My server",
		},
		NotBefore:   start,
		NotAfter:    end,
		IsCA:        false,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafBytes, err := x509.CreateCertificate(rand.Reader, leaf, intermediate, &leafPriv.PublicKey, intermediatePriv)
	require.NoError(t, err)
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafBytes})

	// Write the chain
	out := append(leafPEM, intermediatePEM...)
	out = append(out, caPEM...)
	f, err = os.Create(filepath.Join(tmpDir, "cert.pem"))
	require.NoError(t, err)
	_, err = f.Write(out)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Create the actual test
	certURI := "file://" + filepath.Join(tmpDir, "cert.pem")
	plugin := &X509Cert{
		Sources: []string{certURI},
		ClientConfig: common_tls.ClientConfig{
			TLSCA: filepath.Join(tmpDir, "ca.pem"),
		},
		Log: testutil.Logger{},
	}
	require.NoError(t, plugin.Init())

	var acc testutil.Accumulator
	require.NoError(t, plugin.Gather(&acc))
	require.Empty(t, acc.Errors)

	expected := []telegraf.Metric{
		metric.New(
			"x509_cert",
			map[string]string{
				"common_name":          "My server",
				"country":              "US",
				"issuer_common_name":   "Intermediate CA",
				"issuer_serial_number": "",
				"ocsp_stapled":         "no",
				"organization":         "Testing Inc.",
				"public_key_algorithm": "RSA",
				"san":                  "127.0.0.1",
				"serial_number":        "53950",
				"signature_algorithm":  "SHA256-RSA",
				"source":               filepath.ToSlash(certURI),
				"type":                 "leaf",
				"verification":         "valid",
			},
			map[string]interface{}{
				"age":               int64(0),
				"expiry":            int64(86399),
				"startdate":         start.Unix(),
				"enddate":           end.Unix(),
				"verification_code": int64(0),
			},
			time.Unix(0, 0),
		),
		metric.New(
			"x509_cert",
			map[string]string{
				"common_name":          "Intermediate CA",
				"country":              "US",
				"issuer_common_name":   "Root CA",
				"issuer_serial_number": "",
				"ocsp_stapled":         "no",
				"organization":         "Testing Inc.",
				"public_key_algorithm": "RSA",
				"san":                  "",
				"serial_number":        "5394f",
				"signature_algorithm":  "SHA256-RSA",
				"source":               filepath.ToSlash(certURI),
				"type":                 "intermediate",
				"verification":         "valid",
			},
			map[string]interface{}{
				"age":               int64(0),
				"expiry":            int64(86399),
				"startdate":         start.Unix(),
				"enddate":           end.Unix(),
				"verification_code": int64(0),
			},
			time.Unix(0, 0),
		),
		metric.New(
			"x509_cert",
			map[string]string{
				"common_name":          "Root CA",
				"country":              "US",
				"issuer_common_name":   "Root CA",
				"issuer_serial_number": "",
				"ocsp_stapled":         "no",
				"organization":         "Testing Inc.",
				"public_key_algorithm": "RSA",
				"san":                  "",
				"serial_number":        "5394e",
				"signature_algorithm":  "SHA256-RSA",
				"source":               filepath.ToSlash(certURI),
				"type":                 "root",
				"verification":         "valid",
			},
			map[string]interface{}{
				"age":               int64(0),
				"expiry":            int64(86399),
				"startdate":         start.Unix(),
				"enddate":           end.Unix(),
				"verification_code": int64(0),
			},
			time.Unix(0, 0),
		),
	}

	opts := []cmp.Option{
		testutil.SortMetrics(),
		testutil.IgnoreTime(),
		// We need to ignore those fields as they are timing sensitive.
		testutil.IgnoreFields("age", "expiry"),
	}
	actual := acc.GetTelegrafMetrics()
	testutil.RequireMetricsEqual(t, expected, actual, opts...)
}

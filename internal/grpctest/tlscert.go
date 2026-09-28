package grpctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCert is a self-signed certificate minted for one test.
//
// It exists so TLS tests never need a file fixture, a checked-in key, or a
// listening socket. Certificate is what a fake server presents; CAPEM is
// what a client pins via tls_ca_file. Because the certificate is its own
// issuer, the two are the same bytes viewed from either side.
type TestCert struct {
	Certificate tls.Certificate
	CAPEM       []byte
}

// NewTestCert mints a self-signed certificate valid for hosts.
//
// A host that parses as an IP address lands in IPAddresses and everything
// else in DNSNames. That split is the point: pinning against a bare IP only
// works when the certificate carries an IP SAN, and the client spec treats
// that as a supported case.
func NewTestCert(t *testing.T, hosts ...string) TestCert {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{Organization: []string{"VCS Test"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		// Self-signed and self-issued: the leaf must be a CA for a client
		// pinning it as a root to accept it as its own issuer.
		IsCA: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			continue
		}
		tmpl.DNSNames = append(tmpl.DNSNames, h)
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("build keypair: %v", err)
	}

	return TestCert{Certificate: pair, CAPEM: certPEM}
}

// Pool returns a cert pool trusting only this certificate.
func (c TestCert) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(c.CAPEM)
	return pool
}

// WriteCAPEM writes CAPEM to dir/ca.pem and returns the path, for tests that
// need tls_ca_file to point at a file that genuinely exists.
func (c TestCert) WriteCAPEM(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, c.CAPEM, 0o600); err != nil {
		t.Fatalf("write CA pem: %v", err)
	}
	return path
}

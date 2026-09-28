package grpctest_test

import (
	"crypto/x509"
	"os"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

func TestNewTestCert_VerifiesAgainstItsOwnPool(t *testing.T) {
	cert := grpctest.NewTestCert(t, "localhost", "127.0.0.1")

	leaf, err := x509.ParseCertificate(cert.Certificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	t.Run("DNS name verifies", func(t *testing.T) {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: cert.Pool(), DNSName: "localhost"}); err != nil {
			t.Fatalf("expected localhost to verify: %v", err)
		}
	})

	t.Run("IP SAN is present", func(t *testing.T) {
		// Pinning by bare IP is a supported case, and it only works if the
		// address lands in IPAddresses rather than DNSNames.
		if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "127.0.0.1" {
			t.Fatalf("expected one IP SAN 127.0.0.1, got %v", leaf.IPAddresses)
		}
	})

	t.Run("an unrelated name does not verify", func(t *testing.T) {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: cert.Pool(), DNSName: "evil.example.org"}); err == nil {
			t.Fatal("expected verification against a name not in the SANs to fail")
		}
	})
}

func TestTestCert_PoolRejectsAForeignCert(t *testing.T) {
	// Two independently minted certs must not trust each other, or the
	// "untrusted certificate is refused" tests would pass vacuously.
	a := grpctest.NewTestCert(t, "localhost")
	b := grpctest.NewTestCert(t, "localhost")

	leafB, err := x509.ParseCertificate(b.Certificate.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if _, err := leafB.Verify(x509.VerifyOptions{Roots: a.Pool(), DNSName: "localhost"}); err == nil {
		t.Fatal("expected cert B to be untrusted by pool A")
	}
}

func TestTestCert_WriteCAPEM(t *testing.T) {
	cert := grpctest.NewTestCert(t, "localhost")
	path := cert.WriteCAPEM(t, t.TempDir())

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		t.Fatal("written file is not parseable as a PEM certificate")
	}
}

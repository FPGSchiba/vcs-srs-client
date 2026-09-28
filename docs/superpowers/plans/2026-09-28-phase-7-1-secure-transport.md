# Phase 7.1 Secure Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the VCS client an authenticated, encrypted gRPC control channel to a remote server, so that field verification of Phases 3–6 becomes possible for the first time.

**Architecture:** The client picks transport credentials from the dialed address and one config key — a pinned CA if `tls_ca_file` is set, insecure for loopback, OS trust store otherwise — with no plaintext-remote escape hatch and no downgrade-on-failure fallback. The server gains a top-level `clientTLS` config block that, when populated, wraps its client-facing gRPC listener in TLS. Handshake failures are translated into text a human can act on.

**Tech Stack:** Go 1.x, `google.golang.org/grpc` v1.66.0, `crypto/tls`, `crypto/x509`, `google.golang.org/grpc/credentials`, `google.golang.org/grpc/test/bufconn`, `gopkg.in/yaml.v3` (server), `github.com/BurntSushi/toml` (client).

**Spec:** `docs/superpowers/specs/2026-09-28-vcs-client-phase-7-1-secure-transport-design.md`
**Umbrella spec:** `docs/superpowers/specs/2026-09-28-vcs-client-phase-7-decomposition-design.md`

## Global Constraints

- **Two repositories, two branches, two PRs.** Client tasks (1–5, 9, 10) are in `/Users/schiba/Projects/vanguard/vcs-srs-client` on `feat/phase-7-1-secure-transport` (already created, off `main` at `35b3020`). Server tasks (6–8) are in `/Users/schiba/Projects/vanguard/vngd-srs-server` on a new branch `feat/client-port-tls` off its `main` at `c54b4d4`. **Never commit server changes into the client repo or vice versa.**
- Every client Go invocation carries `-tags purego` and `GOCACHE=$TMPDIR/vcs-gocache`. Example: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/...`
- Server Go invocations carry `GOCACHE=$TMPDIR/vngd-gocache` and **no** `-tags purego` (that tag is a client-only concern). Example: `GOCACHE=$TMPDIR/vngd-gocache go test ./state/...`
- Frontend typecheck, when run: `(cd frontend && npx tsc --noEmit)`. Never `npx --prefix frontend tsc --noEmit` — it prints a help banner and exits 0 without checking anything.
- **No new dependencies in either repo.** Everything here is stdlib or already in `go.mod`. If a task seems to need a dependency, stop and report rather than adding one — dependency changes need the user's explicit approval.
- **No `srs.proto` changes.** Nothing in this sub-phase crosses the wire contract.
- Minimum TLS version is `tls.VersionTLS12` everywhere TLS is configured, client and server.
- The loopback set is exactly: `localhost`, `::1`, and any host starting `127.` — matching the existing `isLocal` in `internal/session/dial.go`. Do not widen it (no RFC1918 ranges): a LAN address is remote and must use TLS.
- gopls is unreliable in the client repo and has reported existing methods as undefined. Never act on its diagnostics; verify with `go build` / `go vet`.
- Shell trap: `cmd | grep -v x; echo $?` reports **grep's** status, not `cmd`'s. Use `${PIPESTATUS[0]}`.

## Review Focus

These are failure modes the spec implies but that no task's happy path exercises. Each has a test assigned to the task owning the code; they are listed here so a reviewer can check they survived.

1. **`tls_ca_file` points at a missing or unreadable file** — must fail the dial with a named error, never silently fall back to system roots. Silent fallback is the security hole this sub-phase exists to close. *(Task 3, Step 1)*
2. **`tls_ca_file` points at a file containing no PEM certificate** (empty file, a private key, random bytes) — must fail with a distinct message, not an empty trust pool that rejects everything with a confusing verification error. *(Task 3, Step 1)*
3. **Server has `certificateFile` but no `privateKeyFile`, or vice versa** — a half-configured TLS block must be a loud startup failure, never a silent fall-through to plaintext. *(Task 7, Step 1)*
4. **IPv6 literal addresses** — `[::1]:5002` must resolve loopback correctly, and `[2001:db8::1]:5002` must be treated as remote and require TLS. `net.SplitHostPort` strips the brackets, so the `isLocal` comparison sees bare `::1`. *(Task 3, Step 1)*
5. **A remote host with an empty `tls_ca_file` and no system-trusted certificate** — must refuse, and the refusal must say what to do about it. This is the exact case a self-hoster hits first. *(Task 5, Step 1)*

---

## Task 1: Client config key and dependency wiring

Adds `tls_ca_file` to the client config and threads it to the session. No behaviour change yet — the value is stored and passed but not read. This is deliberately first because Task 3 cannot compile without `Deps.TLSCAFile` existing.

**Files:**
- Modify: `internal/config/config.go` (add field to `Config`, default in `Default()`)
- Modify: `internal/session/session.go:24-37` (add field to `Deps`)
- Modify: `main.go:121` (pass the config value)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config.TLSCAFile string` (TOML key `tls_ca_file`, default `""`); `session.Deps.TLSCAFile string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestConfig_TLSCAFile(t *testing.T) {
	t.Run("defaults to empty", func(t *testing.T) {
		if got := config.Default().TLSCAFile; got != "" {
			t.Fatalf("expected empty default, got %q", got)
		}
	})

	t.Run("round-trips through TOML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")

		cfg := config.Default()
		cfg.TLSCAFile = "/certs/srs-cert.pem"
		if err := config.Save(path, cfg); err != nil {
			t.Fatalf("save: %v", err)
		}

		loaded, err := config.LoadOrCreate(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if loaded.TLSCAFile != "/certs/srs-cert.pem" {
			t.Fatalf("expected the pinned CA path to survive a round trip, got %q", loaded.TLSCAFile)
		}
	})

	t.Run("absent key loads as empty, not an error", func(t *testing.T) {
		// An older config.toml written before this key existed must still
		// load: the whole point of defaulting every new field.
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte("log_level = \"INFO\"\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		loaded, err := config.LoadOrCreate(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if loaded.TLSCAFile != "" {
			t.Fatalf("expected empty, got %q", loaded.TLSCAFile)
		}
	})
}
```

`config_test.go` is `package config_test` and already imports `os`, `path/filepath` and `testing`, so this needs no import changes. `config.Save(path, *Config) error` and `config.LoadOrCreate(path) (*Config, error)` both exist as used (`internal/config/config.go:378` and `:364`).

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/config/... -run TestConfig_TLSCAFile -v`
Expected: FAIL — `cfg.TLSCAFile undefined (type *config.Config has no field or method TLSCAFile)`

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, inside `type Config struct`, immediately after the `ServerURL` line:

```go
	// TLSCAFile optionally pins the control connection to one certificate
	// authority. Empty (the default) means the OS trust store is used for
	// remote hosts, which is what the public deployment needs. Set it to a
	// PEM file to trust ONLY that issuer -- the case for a self-hosted
	// server with no public DNS and no CA-signed certificate.
	//
	// Setting it also forces TLS for loopback, which is how the local
	// development and test path exercises a real handshake.
	TLSCAFile string `toml:"tls_ca_file"`
```

In `Default()`, immediately after the `ServerURL: "",` line:

```go
		TLSCAFile:           "",
```

- [ ] **Step 4: Add the Deps field**

In `internal/session/session.go`, inside `type Deps struct`, after the `Version string` line:

```go
	// TLSCAFile is config.Config.TLSCAFile, threaded here so dialerFor can
	// reach it. Connect and Reconnect both build dialers, so it belongs on
	// the dependency struct rather than on the Connect call signature.
	TLSCAFile string
```

- [ ] **Step 5: Wire it in main.go**

In `main.go`, replace line 121:

```go
	sess := session.New(gui.Store(), emitter, session.Deps{Version: version.Client})
```

with:

```go
	sess := session.New(gui.Store(), emitter, session.Deps{
		Version:   version.Client,
		TLSCAFile: cfg.TLSCAFile,
	})
```

- [ ] **Step 6: Run the tests and build**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/config/... -run TestConfig_TLSCAFile -v`
Expected: PASS, all three subtests.

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/session/session.go main.go
git commit -m "feat(config): add tls_ca_file and thread it to the session

Stored and passed but not yet read; dialerFor starts honouring it in the
next commit. Defaulted in Default() so configs written before this key
existed still load.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Test-certificate helper

A self-signed certificate minted in-process, so every later test can exercise a genuine TLS handshake without a file on disk or a network listener. It must support IP SANs, because pinning against a bare IP is a supported case that the spec calls out as needing one.

**Files:**
- Create: `internal/grpctest/tlscert.go`
- Test: `internal/grpctest/tlscert_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type TestCert struct { Certificate tls.Certificate; CAPEM []byte }`
  - `func NewTestCert(t *testing.T, hosts ...string) TestCert`
  - `func (c TestCert) Pool() *x509.CertPool`
  - `func (c TestCert) WriteCAPEM(t *testing.T, dir string) string` — writes `CAPEM` to `dir/ca.pem` and returns the path, for tests that need `tls_ca_file` to point at a real file.

- [ ] **Step 1: Write the failing test**

Create `internal/grpctest/tlscert_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/grpctest/... -v`
Expected: FAIL — `undefined: grpctest.NewTestCert`

- [ ] **Step 3: Write the implementation**

Create `internal/grpctest/tlscert.go`:

```go
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
```

- [ ] **Step 4: Run the tests**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/grpctest/... -v`
Expected: PASS, every subtest.

- [ ] **Step 5: Commit**

```bash
git add internal/grpctest/tlscert.go internal/grpctest/tlscert_test.go
git commit -m "test(grpctest): add an in-process self-signed certificate helper

Mints a cert per test with DNS and IP SANs, so TLS tests need no fixture
file and no listening socket. IP SANs matter because pinning by bare
address is a supported client case.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Transport credential selection

The core of the client change: replace `insecureDialer` with credential selection driven by the address and `tls_ca_file`. This task covers Review Focus items 1, 2 and 4.

**Files:**
- Modify: `internal/session/dial.go:48-73` (replace `insecureDialer`)
- Modify: `internal/session/session.go:226` (call site)
- Modify: `internal/session/dial_test.go` (rewrite the existing validation test)

**Interfaces:**
- Consumes: `session.Deps.TLSCAFile` from Task 1.
- Produces:
  - `func transportCredentials(host, caFile string) (credentials.TransportCredentials, error)`
  - `func dialerFor(serverURL, caFile string) (Dialer, error)` — replaces `insecureDialer`, which is deleted.
  - `func isLocal(host string) bool` — unchanged, still used.

- [ ] **Step 1: Write the failing test**

Replace the entire contents of `internal/session/dial_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/credentials"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

// isTLS reports whether creds are TLS rather than insecure, by the protocol
// name the credentials advertise. insecure.NewCredentials() reports "insecure";
// credentials.NewTLS reports "tls".
func isTLS(c credentials.TransportCredentials) bool {
	return c.Info().SecurityProtocol == "tls"
}

func TestTransportCredentials_BranchSelection(t *testing.T) {
	// A real certificate on disk, so the pinned branch is exercised with
	// something AppendCertsFromPEM genuinely accepts rather than a fixture
	// that could drift out of validity.
	caPath := grpctest.NewTestCert(t, "localhost").WriteCAPEM(t, t.TempDir())

	tests := []struct {
		name    string
		host    string
		caFile  string
		wantTLS bool
	}{
		{"loopback name is insecure", "localhost", "", false},
		{"loopback v4 is insecure", "127.0.0.1", "", false},
		{"loopback v4 subnet is insecure", "127.0.0.53", "", false},
		{"loopback v6 is insecure", "::1", "", false},
		{"remote name uses TLS", "srs.example.org", "", true},
		{"remote v4 uses TLS", "203.0.113.10", "", true},
		{"remote v6 uses TLS", "2001:db8::1", "", true},
		{"LAN address is remote, so TLS", "192.168.1.10", "", true},
		{"pinned CA forces TLS on loopback", "localhost", caPath, true},
		{"pinned CA on a remote host uses TLS", "srs.example.org", caPath, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			creds, err := transportCredentials(tc.host, tc.caFile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := isTLS(creds); got != tc.wantTLS {
				t.Fatalf("host=%q caFile set=%v: wantTLS=%v got=%v", tc.host, tc.caFile != "", tc.wantTLS, got)
			}
		})
	}
}

func TestTransportCredentials_BadCAFile(t *testing.T) {
	dir := t.TempDir()

	emptyPath := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(emptyPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	junkPath := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(junkPath, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name    string
		caFile  string
		wantErr string
	}{
		// Review Focus 1: a missing file must fail loudly. Falling back to
		// system roots here would silently ignore an explicit pin.
		{"missing file", filepath.Join(dir, "nope.pem"), "read tls_ca_file"},
		// Review Focus 2: an empty or unparseable file must be distinct from
		// "missing", and must not yield an empty pool that rejects everything
		// with a confusing verification error at dial time.
		{"empty file", emptyPath, "no PEM certificate"},
		{"not a certificate", junkPath, "no PEM certificate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := transportCredentials("srs.example.org", tc.caFile)
			if err == nil {
				t.Fatal("expected an error, got nil -- a bad pin must never fall back to system roots")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestDialerFor_AddressValidation(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		wantErr   string // substring; "" means a dialer is returned
	}{
		{"empty", "", "empty"},
		{"no port", "localhost", "host:port"},
		{"localhost ok", "localhost:5002", ""},
		{"127.0.0.1 ok", "127.0.0.1:5002", ""},
		{"ipv6 loopback ok", "[::1]:5002", ""},
		// Review Focus 4: previously rejected outright. Now it is accepted
		// and will be dialed over TLS -- the fail-closed rule inverted
		// rather than relaxed.
		{"remote accepted, over TLS", "srs.example.org:5002", ""},
		{"remote ipv6 accepted", "[2001:db8::1]:5002", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := dialerFor(tc.serverURL, "")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected a dialer, got error: %v", err)
				}
				if d == nil {
					t.Fatal("expected non-nil dialer")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/... -run 'TestTransportCredentials|TestDialerFor' -v`
Expected: FAIL — `undefined: transportCredentials` and `undefined: dialerFor`

- [ ] **Step 3: Write the implementation**

In `internal/session/dial.go`, replace the entire `insecureDialer` function (lines 48–68, from the `// insecureDialer returns...` comment through its closing brace) with:

```go
// transportCredentials picks the transport for host, honouring caFile.
//
// Three deterministic branches, in this order:
//
//  1. caFile set    -> TLS trusting ONLY that pool, whatever the host. This
//     is what a self-hosted server with no public DNS and no CA-signed
//     certificate needs, and because it is not gated on the host it doubles
//     as the local-TLS development and test path.
//  2. host loopback -> insecure, unchanged from Phase 1.
//  3. otherwise     -> TLS against the OS trust store.
//
// There is deliberately no plaintext-remote escape hatch: the Phase 1
// fail-closed rule inverts rather than relaxes, so what used to be refused
// outright is now required to be encrypted. There is equally no fallback to
// plaintext when a handshake fails, because a fallback is a downgrade attack
// with extra steps.
//
// A caFile that cannot be read, or that holds no certificate, is an ERROR
// rather than a quiet fall-through to system roots. An explicit pin that
// silently stops pinning is the precise failure this sub-phase exists to
// prevent.
func transportCredentials(host, caFile string) (credentials.TransportCredentials, error) {
	if caFile != "" {
		pemBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read tls_ca_file %q: %w", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("tls_ca_file %q contains no PEM certificate", caFile)
		}
		return credentials.NewTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}), nil
	}
	if isLocal(host) {
		return insecure.NewCredentials(), nil
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}), nil
}

// dialerFor returns a Dialer for serverURL with credentials chosen by
// transportCredentials. It replaces Phase 1's insecureDialer.
func dialerFor(serverURL, caFile string) (Dialer, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server address is empty")
	}
	host, _, err := net.SplitHostPort(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server address must be host:port: %w", err)
	}
	creds, err := transportCredentials(host, caFile)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(creds),
			keepaliveOption(),
			grpc.WithBlock(),
		)
	}, nil
}
```

Update the import block at the top of `dial.go` to:

```go
import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)
```

- [ ] **Step 4: Update the call site**

In `internal/session/session.go`, replace line 226:

```go
		d, err := insecureDialer(serverURL)
```

with:

```go
		d, err := dialerFor(serverURL, s.dep.TLSCAFile)
```

- [ ] **Step 5: Run the tests**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/... -v`
Expected: PASS. The whole package must pass, not just the new tests — `session_test.go`, `liveness_test.go` and `reconnect_test.go` all inject their own dialer via `Deps.Dialer` and should be unaffected.

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/session/dial.go internal/session/dial_test.go internal/session/session.go
git commit -m "feat(session): choose transport credentials from address and tls_ca_file

Replaces insecureDialer. The Phase 1 fail-closed rule inverts rather than
relaxes: a remote host used to be refused and is now required to be
encrypted. A pinned CA applies to any host, which is also the local-TLS
test path.

An unreadable or certificate-free tls_ca_file is an error, never a quiet
fall-through to system roots -- an explicit pin that stops pinning without
saying so is the failure this change exists to prevent.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: TLS integration over bufconn

Proves the credentials from Task 3 complete a genuine TLS handshake, and that the two mismatch cases fail. TLS is a byte stream over the connection, so `bufconn` exercises the real handshake with no listening socket — which matters because the sandbox blocks `bind(2)`.

**Files:**
- Modify: `internal/grpctest/fakeserver.go:151-166` (add `StartWith`, reimplement `Start` on top of it)
- Create: `internal/session/tls_integration_test.go`

**Interfaces:**
- Consumes: `grpctest.TestCert`, `grpctest.NewTestCert`, `TestCert.Pool` (Task 2).
- Produces: `func StartWith(t *testing.T, f *Fake, serverCreds credentials.TransportCredentials) (dialWith func(context.Context, credentials.TransportCredentials) (*grpc.ClientConn, error), cleanup func())`. `serverCreds == nil` means a plaintext server. `Start` keeps its existing signature and behaviour.

- [ ] **Step 1: Write the failing test**

Create `internal/session/tls_integration_test.go`:

```go
package session

import (
	"context"
	"crypto/tls"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
)

func dialCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestTLS_TrustedCertificateConnects(t *testing.T) {
	cert := grpctest.NewTestCert(t, "localhost")
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, credentials.NewServerTLSFromCert(&cert.Certificate))
	t.Cleanup(cleanup)

	// ServerName is set explicitly because bufconn's target is "bufnet",
	// which is not in the certificate. This is the same override a client
	// pinning by hostname relies on.
	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(dialCtx(t), clientCreds)
	if err != nil {
		t.Fatalf("expected the handshake to succeed against a trusted cert: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
}

func TestTLS_UntrustedCertificateIsRefused(t *testing.T) {
	serverCert := grpctest.NewTestCert(t, "localhost")
	otherCert := grpctest.NewTestCert(t, "localhost") // a different issuer

	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, credentials.NewServerTLSFromCert(&serverCert.Certificate))
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    otherCert.Pool(), // pins the WRONG issuer
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(dialCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a certificate signed by an untrusted issuer to be refused")
	}
}

func TestTLS_HostnameMismatchIsRefused(t *testing.T) {
	cert := grpctest.NewTestCert(t, "srs.example.org") // no "localhost" SAN
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, credentials.NewServerTLSFromCert(&cert.Certificate))
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(), // issuer IS trusted; only the name is wrong
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(dialCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a certificate valid for a different name to be refused")
	}
	if !strings.Contains(err.Error(), "x509") {
		t.Fatalf("expected an x509 verification failure, got: %v", err)
	}
}

func TestTLS_ClientAgainstPlaintextServerFails(t *testing.T) {
	// The failure an operator hits when they upgrade the client before the
	// server, or forget the clientTLS block entirely.
	cert := grpctest.NewTestCert(t, "localhost")
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, nil) // plaintext server
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := dialWith(dialCtx(t), clientCreds)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected a TLS client against a plaintext server to fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/... -run TestTLS -v`
Expected: FAIL — `undefined: grpctest.StartWith`

- [ ] **Step 3: Implement StartWith**

In `internal/grpctest/fakeserver.go`, replace the existing `Start` function (from its `// Start launches the fake...` comment through its closing brace) with:

```go
// Start launches the fake on a plaintext bufconn and returns a dialer +
// cleanup. Preserved for the tests written before TLS existed.
func Start(t *testing.T, f *Fake) (dial func(context.Context) (*grpc.ClientConn, error), cleanup func()) {
	t.Helper()
	dialWith, cleanup := StartWith(t, f, nil)
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return dialWith(ctx, insecure.NewCredentials())
	}, cleanup
}

// StartWith launches the fake on a bufconn with the given SERVER transport
// credentials (nil means plaintext) and returns a dialer that lets each test
// choose its own CLIENT credentials.
//
// Splitting the two sides is what makes the mismatch cases testable at all:
// a TLS client against a plaintext server, and a client pinning the wrong
// issuer. And running TLS over bufconn rather than a real socket is
// deliberate -- TLS is just a byte stream over the conn, so the genuine
// handshake runs with no call to bind(2), which the development sandbox
// blocks.
func StartWith(t *testing.T, f *Fake, serverCreds credentials.TransportCredentials) (dialWith func(context.Context, credentials.TransportCredentials) (*grpc.ClientConn, error), cleanup func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)

	var opts []grpc.ServerOption
	if serverCreds != nil {
		opts = append(opts, grpc.Creds(serverCreds))
	}
	srv := grpc.NewServer(opts...)
	srspb.RegisterAuthServiceServer(srv, f)
	srspb.RegisterSRSServiceServer(srv, f)
	go func() { _ = srv.Serve(lis) }()

	dialWith = func(ctx context.Context, clientCreds credentials.TransportCredentials) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, "bufnet",
			grpc.WithContextDialer(func(c context.Context, _ string) (net.Conn, error) { return lis.DialContext(c) }),
			grpc.WithTransportCredentials(clientCreds),
			// Without this a failed handshake surfaces as a bare context
			// deadline and the real cause is lost, which would make the
			// refusal tests below unable to tell a rejection from a timeout.
			grpc.WithReturnConnectionError(),
		)
	}
	cleanup = func() { srv.Stop(); _ = lis.Close() }
	return dialWith, cleanup
}
```

Add `"google.golang.org/grpc/credentials"` to the import block in `fakeserver.go`. `insecure`, `net`, `grpc` and `bufconn` are already imported.

- [ ] **Step 4: Run the tests**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/... ./internal/grpctest/... -v`
Expected: PASS. All four `TestTLS_*` tests pass, and every pre-existing test that used `Start` still passes.

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/session/... ./internal/grpctest/...`
Expected: PASS, no race reports.

- [ ] **Step 5: Commit**

```bash
git add internal/grpctest/fakeserver.go internal/session/tls_integration_test.go
git commit -m "test(session): exercise the real TLS handshake over bufconn

StartWith splits server and client credentials so the mismatch cases are
testable: an untrusted issuer, a hostname mismatch, and a TLS client
against a plaintext server.

TLS over bufconn rather than a socket is deliberate -- the handshake is
genuine but needs no bind(2), which the sandbox blocks.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Actionable dial errors

Turns opaque transport failures into text a human can act on. Covers Review Focus item 5.

**Files:**
- Modify: `internal/session/dial.go` (add `explainDialError`, use it in `dialerFor`)
- Create: `internal/session/dialerror_test.go`

**Interfaces:**
- Consumes: `dialerFor` (Task 3), `grpctest.StartWith` (Task 4).
- Produces: `func explainDialError(err error, serverURL, caFile string) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/session/dialerror_test.go`:

```go
package session

import (
	"errors"
	"strings"
	"testing"
)

func TestExplainDialError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		serverURL string
		caFile    string
		want      []string // all must appear in the message
	}{
		{
			name:      "plaintext server",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: tls: first record does not look like a TLS handshake"`),
			serverURL: "srs.example.org:5002",
			want:      []string{"srs.example.org:5002", "not speaking TLS", "clientTLS"},
		},
		{
			// Review Focus 5: the first failure a self-hoster hits. The
			// message has to name the remedy, not just the symptom.
			name:      "untrusted issuer, no pin configured",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: x509: certificate signed by unknown authority"`),
			serverURL: "srs.example.org:5002",
			want:      []string{"srs.example.org:5002", "tls_ca_file"},
		},
		{
			name:      "untrusted issuer with a pin configured",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: x509: certificate signed by unknown authority"`),
			serverURL: "srs.example.org:5002",
			caFile:    "/certs/ca.pem",
			want:      []string{"/certs/ca.pem", "not issued by"},
		},
		{
			name:      "hostname mismatch",
			err:       errors.New(`connection error: desc = "transport: authentication handshake failed: x509: certificate is valid for srs.example.org, not 203.0.113.10"`),
			serverURL: "203.0.113.10:5002",
			want:      []string{"203.0.113.10:5002", "different name"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := explainDialError(tc.err, tc.serverURL, tc.caFile)
			for _, want := range tc.want {
				if !strings.Contains(got.Error(), want) {
					t.Fatalf("message %q does not contain %q", got.Error(), want)
				}
			}
			// The original must stay reachable: the wrapped text is for the
			// user, the cause is for the log.
			if !errors.Is(got, tc.err) {
				t.Fatal("expected the original error to remain unwrappable via errors.Is")
			}
		})
	}
}

func TestExplainDialError_UnrecognisedErrorPassesThrough(t *testing.T) {
	// The substring matching below is the only signal available, so an
	// unrecognised failure must degrade to the raw error rather than being
	// swallowed or mislabelled.
	orig := errors.New("connection refused")
	got := explainDialError(orig, "srs.example.org:5002", "")
	if got != orig {
		t.Fatalf("expected the original error unchanged, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/... -run TestExplainDialError -v`
Expected: FAIL — `undefined: explainDialError`

- [ ] **Step 3: Write the implementation**

Append to `internal/session/dial.go`:

```go
// explainDialError turns a gRPC transport failure into something a user can
// act on.
//
// The cases are matched on crypto/tls and crypto/x509 error TEXT because
// gRPC surfaces them as an opaque wrapped string with no typed cause to
// inspect -- there is no errors.As target available here. That is fragile by
// nature, so the default is the original error: a change in Go's wording
// degrades the message rather than hiding the failure or mislabelling it.
//
// The original is always wrapped, never replaced, so errors.Is still reaches
// it for the log.
func explainDialError(err error, serverURL, caFile string) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "first record does not look like a TLS handshake"):
		return fmt.Errorf("%s is not speaking TLS -- the server most likely has no clientTLS block configured: %w", serverURL, err)

	case strings.Contains(s, "certificate is valid for"):
		return fmt.Errorf("%s presented a certificate for a different name -- connect by the server's hostname, or have its certificate reissued with this address in the SANs: %w", serverURL, err)

	case strings.Contains(s, "certificate signed by unknown authority"),
		strings.Contains(s, "failed to verify certificate"):
		if caFile != "" {
			return fmt.Errorf("%s presented a certificate not issued by the authority in tls_ca_file %q: %w", serverURL, caFile, err)
		}
		return fmt.Errorf("%s presented a certificate that no system root trusts -- if this is a self-hosted server, point tls_ca_file at its certificate: %w", serverURL, err)
	}
	return err
}
```

Note the case ordering: "certificate is valid for" is checked **before** the unknown-authority case, because a message can contain both and the name mismatch is the more specific, more actionable diagnosis.

- [ ] **Step 4: Use it in dialerFor, and surface the cause**

In `internal/session/dial.go`, inside `dialerFor`, replace the returned closure:

```go
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(creds),
			keepaliveOption(),
			grpc.WithBlock(),
		)
	}, nil
```

with:

```go
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		conn, err := grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(creds),
			keepaliveOption(),
			// Replaces WithBlock. It blocks identically, but returns the last
			// connection error instead of a bare context deadline. Without it
			// every TLS failure reaches the user as "context deadline
			// exceeded" and explainDialError has nothing to work with.
			grpc.WithReturnConnectionError(),
		)
		if err != nil {
			return nil, explainDialError(err, serverURL, caFile)
		}
		return conn, nil
	}, nil
```

- [ ] **Step 5: Add an end-to-end assertion on the wrapped message**

Append to `internal/session/tls_integration_test.go`:

```go
func TestTLS_PlaintextServerProducesTheExplainedError(t *testing.T) {
	// Proves the wrapping is actually reached through the real dial path,
	// not merely unit-tested in isolation -- and that
	// WithReturnConnectionError really does surface the TLS cause rather
	// than a context deadline.
	cert := grpctest.NewTestCert(t, "localhost")
	dialWith, cleanup := grpctest.StartWith(t, &grpctest.Fake{}, nil) // plaintext
	t.Cleanup(cleanup)

	clientCreds := credentials.NewTLS(&tls.Config{
		RootCAs:    cert.Pool(),
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	_, err := dialWith(dialCtx(t), clientCreds)
	if err == nil {
		t.Fatal("expected the dial to fail")
	}
	explained := explainDialError(err, "srs.example.org:5002", "")
	if !strings.Contains(explained.Error(), "not speaking TLS") {
		t.Fatalf("expected the plaintext-server diagnosis, got: %v", explained)
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/session/... ./internal/grpctest/... -v`
Expected: PASS, including the new end-to-end assertion.

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/session/dial.go internal/session/dialerror_test.go internal/session/tls_integration_test.go
git commit -m "feat(session): explain TLS dial failures in terms a user can act on

Three diagnoses -- server not speaking TLS, untrusted issuer, hostname
mismatch -- each naming the remedy. WithReturnConnectionError replaces
WithBlock so the TLS cause survives instead of collapsing into a context
deadline.

Matching is on error text because gRPC exposes no typed cause; the default
is the original error, so a wording change degrades the message rather than
mislabelling the failure.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Server clientTLS settings block

**This task and the two after it are in `/Users/schiba/Projects/vanguard/vngd-srs-server`, not the client repo.**

Before starting, create the branch:

```bash
cd /Users/schiba/Projects/vanguard/vngd-srs-server
git checkout main && git pull --ff-only 2>/dev/null || git checkout main
git checkout -b feat/client-port-tls
```

**Files:**
- Modify: `state/settings.go` (add `ClientTLSSettings`, add the field to `SettingsState`)
- Modify: `state/snapshot.go` (carry it into `SettingsSnapshot`)
- Modify: `example.config.yaml`
- Test: `state/settings_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type ClientTLSSettings struct { CertificateFile string; PrivateKeyFile string; ServerName string }` with yaml keys `certificateFile`, `privateKeyFile`, `serverName`.
  - `SettingsState.ClientTLS ClientTLSSettings` (yaml key `clientTLS`).
  - `SettingsSnapshot.ClientTLS ClientTLSSettings`.

- [ ] **Step 1: Write the failing test**

Append to `state/settings_test.go`:

```go
func TestSettingsState_ClientTLSIsTopLevel(t *testing.T) {
	// clientTLS is top-level rather than nested under servers.control on
	// purpose. app.SaveServerSettings assigns SettingsState.Servers =
	// *newSettings wholesale, and the GraphQL resolver rebuilds
	// ServerSettings from input carrying only host and port -- so TLS nested
	// inside Servers would be erased by any admin settings save and then
	// persisted as plaintext. This test pins the placement.
	s := &state.SettingsState{
		Servers: state.ServerSettings{
			Control: state.ServerSetting{Host: "0.0.0.0", Port: 5002},
		},
		ClientTLS: state.ClientTLSSettings{
			CertificateFile: "/certs/srs-cert.pem",
			PrivateKeyFile:  "/certs/srs-private-key.pem",
			ServerName:      "vcs.vngd.net",
		},
	}

	// Simulate the admin write path: replace Servers wholesale.
	s.Servers = state.ServerSettings{
		Control: state.ServerSetting{Host: "0.0.0.0", Port: 5002},
	}

	if s.ClientTLS.CertificateFile != "/certs/srs-cert.pem" {
		t.Fatal("clientTLS must survive a wholesale replacement of Servers")
	}
}

func TestSettingsState_ClientTLSSnapshot(t *testing.T) {
	s := &state.SettingsState{
		ClientTLS: state.ClientTLSSettings{
			CertificateFile: "/certs/srs-cert.pem",
			PrivateKeyFile:  "/certs/srs-private-key.pem",
			ServerName:      "vcs.vngd.net",
		},
	}
	snap := s.Snapshot()
	if snap.ClientTLS.CertificateFile != "/certs/srs-cert.pem" {
		t.Fatalf("expected the cert path in the snapshot, got %q", snap.ClientTLS.CertificateFile)
	}
	if snap.ClientTLS.ServerName != "vcs.vngd.net" {
		t.Fatalf("expected the server name in the snapshot, got %q", snap.ClientTLS.ServerName)
	}
}

func TestSettingsState_ClientTLSYAMLRoundTrip(t *testing.T) {
	in := []byte(
		"clientTLS:\n" +
			"  certificateFile: /certs/srs-cert.pem\n" +
			"  privateKeyFile: /certs/srs-private-key.pem\n" +
			"  serverName: vcs.vngd.net\n")

	var s state.SettingsState
	if err := yaml.Unmarshal(in, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.ClientTLS.PrivateKeyFile != "/certs/srs-private-key.pem" {
		t.Fatalf("expected the key path, got %q", s.ClientTLS.PrivateKeyFile)
	}

	// A config with no clientTLS block must load cleanly as the zero value:
	// plaintext, which is what every existing deployment has today.
	var absent state.SettingsState
	if err := yaml.Unmarshal([]byte("general:\n  maxRadiosPerUser: 10\n"), &absent); err != nil {
		t.Fatalf("unmarshal without clientTLS: %v", err)
	}
	if absent.ClientTLS.CertificateFile != "" {
		t.Fatalf("expected empty, got %q", absent.ClientTLS.CertificateFile)
	}
}
```

Add `"gopkg.in/yaml.v3"` to the imports of `state/settings_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vngd-gocache go test ./state/... -run ClientTLS -v`
Expected: FAIL — `undefined: state.ClientTLSSettings`

- [ ] **Step 3: Add the settings type**

In `state/settings.go`, add the field to `SettingsState` immediately after the `VoiceControl` line:

```go
	ClientTLS    ClientTLSSettings    `yaml:"clientTLS"`
```

And add the type immediately after `VoiceControlSettings`:

```go
// ClientTLSSettings configures TLS on the CLIENT-FACING gRPC listener --
// the one serving SRSService and AuthService, which the VCS client dials.
// It is distinct from VoiceControlSettings, whose certificate secures the
// server-to-server VoiceControl channel on a different port entirely.
//
// Leaving CertificateFile or PrivateKeyFile empty means plaintext, which is
// what every deployment predating this block already has.
//
// This is deliberately TOP-LEVEL rather than nested under
// servers.control.tls. SaveServerSettings in app/settings.go assigns
// SettingsState.Servers = *newSettings wholesale, and the GraphQL resolver
// rebuilds ServerSettings from an input carrying only host and port -- so a
// TLS block living inside Servers would be silently erased by any admin
// settings save and then written to disk as plaintext, downgrading the
// server on its next restart.
type ClientTLSSettings struct {
	CertificateFile string `yaml:"certificateFile"`
	PrivateKeyFile  string `yaml:"privateKeyFile"`
	// ServerName is used as an additional SAN when the pair is generated,
	// and is what clients verify against.
	ServerName string `yaml:"serverName"`
}
```

- [ ] **Step 4: Carry it into the snapshot**

In `state/snapshot.go`, add to `SettingsSnapshot` after the `VoiceControl` field:

```go
	ClientTLS    ClientTLSSettings
```

and to the struct literal in `Snapshot()` after the `VoiceControl:` line:

```go
		ClientTLS:    s.ClientTLS,
```

- [ ] **Step 5: Document it in the example config**

In `example.config.yaml`, add immediately before the `api:` block:

```yaml
clientTLS: # TLS for the client-facing control port (servers.control). Omit the block for plaintext.
  certificateFile: /path/to/srs-cert.pem # Generated at this location if not present
  privateKeyFile: /path/to/srs-private-key.pem # Generated at this location if not present
  serverName: vcs.vngd.net # Added to the generated certificate's SANs; clients verify against this name
```

- [ ] **Step 6: Run the tests**

Run: `GOCACHE=$TMPDIR/vngd-gocache go test ./state/... -v`
Expected: PASS, all three new tests and every pre-existing one.

Run: `GOCACHE=$TMPDIR/vngd-gocache go build ./... && GOCACHE=$TMPDIR/vngd-gocache go vet ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add state/settings.go state/snapshot.go state/settings_test.go example.config.yaml
git commit -m "feat(state): add a top-level clientTLS settings block

Configures TLS on the client-facing gRPC listener, which today has no
grpc.Creds at all -- distinct from the VoiceControl certificate, which
secures a different port.

Top-level rather than nested under servers.control because
SaveServerSettings replaces SettingsState.Servers wholesale from a GraphQL
input carrying only host and port, so a nested block would be erased by any
admin settings save and persisted as plaintext.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Serve the client port over TLS

Covers Review Focus item 3.

**Files:**
- Modify: `control/server.go` (add `clientTransportCredentials`, use it at the `clientGrpcServer` construction, currently line 97)
- Test: `control/server_test.go`

**Interfaces:**
- Consumes: `state.ClientTLSSettings` (Task 6).
- Produces: `func clientTransportCredentials(cfg state.ClientTLSSettings, logger *slog.Logger) (credentials.TransportCredentials, error)` — unexported; `control/server_test.go` is `package control`, an internal test, so it calls this directly with no qualifier — returns `(nil, nil)` for "not configured, serve plaintext", `(creds, nil)` when configured, and `(nil, err)` when half-configured or the keypair cannot be loaded.

- [ ] **Step 1: Write the failing test**

Append to `control/server_test.go`:

```go
func TestClientTransportCredentials(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()

	t.Run("unconfigured means plaintext", func(t *testing.T) {
		creds, err := clientTransportCredentials(state.ClientTLSSettings{}, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds != nil {
			t.Fatal("expected nil credentials so the listener stays plaintext")
		}
	})

	// Review Focus 3: a half-configured block must be a loud failure. Falling
	// through to plaintext here would give an operator who thought they
	// enabled TLS a server that quietly did not.
	t.Run("certificate without key is an error", func(t *testing.T) {
		_, err := clientTransportCredentials(state.ClientTLSSettings{
			CertificateFile: filepath.Join(dir, "cert.pem"),
		}, logger)
		if err == nil {
			t.Fatal("expected half-configured TLS to fail, not fall back to plaintext")
		}
		if !strings.Contains(err.Error(), "privateKeyFile") {
			t.Fatalf("error should name the missing field, got: %v", err)
		}
	})

	t.Run("key without certificate is an error", func(t *testing.T) {
		_, err := clientTransportCredentials(state.ClientTLSSettings{
			PrivateKeyFile: filepath.Join(dir, "key.pem"),
		}, logger)
		if err == nil {
			t.Fatal("expected half-configured TLS to fail, not fall back to plaintext")
		}
		if !strings.Contains(err.Error(), "certificateFile") {
			t.Fatalf("error should name the missing field, got: %v", err)
		}
	})

	t.Run("fully configured generates and returns credentials", func(t *testing.T) {
		certPath := filepath.Join(dir, "gen-cert.pem")
		keyPath := filepath.Join(dir, "gen-key.pem")

		creds, err := clientTransportCredentials(state.ClientTLSSettings{
			CertificateFile: certPath,
			PrivateKeyFile:  keyPath,
			ServerName:      "vcs.test",
		}, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds == nil {
			t.Fatal("expected credentials")
		}
		if creds.Info().SecurityProtocol != "tls" {
			t.Fatalf("expected tls, got %q", creds.Info().SecurityProtocol)
		}
		// The pair is generated on first use, matching the VoiceControl
		// channel's behaviour, so a self-hoster gets something to copy.
		if _, err := os.Stat(certPath); err != nil {
			t.Fatalf("expected the certificate to be generated at %s: %v", certPath, err)
		}
		if _, err := os.Stat(keyPath); err != nil {
			t.Fatalf("expected the key to be generated at %s: %v", keyPath, err)
		}
	})

	t.Run("a second call reuses the generated pair", func(t *testing.T) {
		certPath := filepath.Join(dir, "reuse-cert.pem")
		keyPath := filepath.Join(dir, "reuse-key.pem")
		cfg := state.ClientTLSSettings{CertificateFile: certPath, PrivateKeyFile: keyPath, ServerName: "vcs.test"}

		if _, err := clientTransportCredentials(cfg, logger); err != nil {
			t.Fatalf("first call: %v", err)
		}
		first, err := os.ReadFile(certPath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if _, err := clientTransportCredentials(cfg, logger); err != nil {
			t.Fatalf("second call: %v", err)
		}
		second, err := os.ReadFile(certPath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(first) != string(second) {
			t.Fatal("a restart must not mint a new certificate -- clients pinning the old one would break")
		}
	})
}
```

`control/server_test.go` is `package control` — an internal test — so the helper is called unqualified and stays unexported. Add whatever of `io`, `log/slog`, `os`, `path/filepath`, `strings` and `github.com/FPGSchiba/vcs-srs-server/state` are not already imported there.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vngd-gocache go test ./control/... -run TestClientTransportCredentials -v`
Expected: FAIL — `undefined: clientTransportCredentials`

- [ ] **Step 3: Write the implementation**

Add to `control/server.go`. Every import it needs — `fmt`, `log/slog`, `state`, `credentials`, `voiceontrol` — is already in that file's import block, so no import changes are required:

```go
// clientTransportCredentials builds transport credentials for the
// client-facing gRPC listener from cfg.
//
// Returns (nil, nil) when TLS is not configured, which leaves the listener
// plaintext -- the behaviour every deployment predating this block already
// has. Returns an error when the block is HALF configured, because an
// operator who set one field of two believes they enabled TLS, and silently
// serving plaintext to that operator is worse than refusing to start.
//
// The keypair is generated at the configured paths if absent, reusing
// LoadOrGenerateKeyPair exactly as the VoiceControl channel does, so a
// self-hoster ends up with a certificate they can copy to their clients.
// Regeneration only happens when the files are missing: a restart must not
// mint a new certificate, or every client pinning the old one breaks.
func clientTransportCredentials(cfg state.ClientTLSSettings, logger *slog.Logger) (credentials.TransportCredentials, error) {
	switch {
	case cfg.CertificateFile == "" && cfg.PrivateKeyFile == "":
		logger.Warn("client-facing gRPC port is PLAINTEXT: no clientTLS block configured, so client credentials cross the network in the clear")
		return nil, nil
	case cfg.PrivateKeyFile == "":
		return nil, fmt.Errorf("clientTLS.certificateFile is set but clientTLS.privateKeyFile is empty: configure both or neither")
	case cfg.CertificateFile == "":
		return nil, fmt.Errorf("clientTLS.privateKeyFile is set but clientTLS.certificateFile is empty: configure both or neither")
	}

	cert, _, err := voiceontrol.LoadOrGenerateKeyPair(cfg.PrivateKeyFile, cfg.CertificateFile, cfg.ServerName)
	if err != nil {
		return nil, fmt.Errorf("load or generate client TLS keypair: %w", err)
	}
	logger.Info("client-facing gRPC port is TLS", "certificateFile", cfg.CertificateFile, "serverName", cfg.ServerName)
	return credentials.NewServerTLSFromCert(cert), nil
}
```

- [ ] **Step 4: Wire it into the listener**

In `control/server.go` `Start`, immediately before the `s.clientGrpcServer = grpc.NewServer(` assignment (currently line 97), read the settings and build the options:

```go
	s.settingsState.RLock()
	clientTLS := s.settingsState.ClientTLS
	s.settingsState.RUnlock()

	clientCreds, err := clientTransportCredentials(clientTLS, s.logger)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("client TLS: %w", err)
	}

	clientOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(s.loggingInterceptor, s.authInterceptor),
		grpc.ChainStreamInterceptor(s.authStreamInterceptor),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             60 * time.Second, // allow pings every 60s
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    60 * time.Second, // server sends pings every 30s if idle
			Timeout: 10 * time.Second, // wait 10s for ping ack
		}),
	}
	if clientCreds != nil {
		clientOpts = append(clientOpts, grpc.Creds(clientCreds))
	}
```

Then replace the existing `s.clientGrpcServer = grpc.NewServer(...)` call and its whole option list with:

```go
	s.clientGrpcServer = grpc.NewServer(clientOpts...)
```

Note the explicit `s.mu.Unlock()` before the error return. `Start` holds `s.mu` from before the listener setup through to `s.isRunning = true`. **Do not copy the neighbouring error paths here:** the existing `net.Listen` failure returns at that point *without* unlocking, which leaks the mutex. That is a pre-existing defect, it is out of scope for this task, and you must not propagate it into the new path — unlock explicitly as shown. Do not fix the pre-existing leak either; it is recorded separately for the final review.

- [ ] **Step 5: Run the tests**

Run: `GOCACHE=$TMPDIR/vngd-gocache go test ./control/... ./state/... -v`
Expected: PASS.

Run: `GOCACHE=$TMPDIR/vngd-gocache go test -race ./... 2>&1 | tail -30`
Expected: no failures, no race reports. Some packages may be slow; that is fine.

Run: `GOCACHE=$TMPDIR/vngd-gocache go build ./... && GOCACHE=$TMPDIR/vngd-gocache go vet ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add control/server.go control/server_test.go
git commit -m "feat(control): serve the client-facing gRPC port over TLS

clientGrpcServer -- the listener registering SRSService and AuthService,
the one the VCS client dials -- has had no grpc.Creds since it was written,
and no option to add any. Every TLS artefact in this repo serves the
server-to-server VoiceControl channel on a different port.

Unconfigured stays plaintext with a startup WARN, so existing deployments
are unaffected. A half-configured block fails startup rather than serving
plaintext to an operator who believes they enabled TLS.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Server deployment documentation

**Files:**
- Modify: `deploy/README.md`

**Interfaces:**
- Consumes: the `clientTLS` block from Task 6.
- Produces: nothing code-facing.

- [ ] **Step 1: Add the client-port TLS section**

`deploy/README.md` currently documents certificates only for the voice-control channel, which has caused the client-side documentation to claim the deployed server "already has certs" when those certs serve a different port. Add a new section after the existing "Certs directory" section. The outer fence below is four backticks because the content itself contains a fenced yaml block — what you paste into `deploy/README.md` is everything between the four-backtick markers, inner fences included:

````markdown
### Client-facing TLS

The `certs/` material described above secures the **voice-control channel
between nodes**. It does nothing for the port your users' clients connect
to. That port is configured separately:

```yaml
clientTLS:
  certificateFile: /certs/srs-cert.pem
  privateKeyFile:  /certs/srs-private-key.pem
  serverName:      vcs.vngd.net
```

Omit the block entirely to serve plaintext. The server logs a WARN at
startup when you do, because client credentials then cross the network in
the clear. Setting one of `certificateFile` / `privateKeyFile` without the
other fails startup rather than quietly serving plaintext.

**Public deployments** should point these at a certificate issued by a real
CA for the hostname users type. Clients then verify against their OS trust
store with no configuration at all.

**Self-hosted or LAN deployments** can leave the files absent: the server
generates a self-signed pair at those paths on first start, the same way the
Control node generates its voice-control pair. Copy the **certificate only**
(`srs-cert.pem`) to each client and point that client's `tls_ca_file` at it.

> Clients connecting by bare IP rather than hostname need that address in the
> certificate's SANs. Set `serverName` to the IP before first start so the
> generated certificate carries it — the pair is only generated when the
> files are absent, so changing `serverName` later has no effect until you
> delete them.
````

- [ ] **Step 2: Verify the markdown renders**

Run: `GOCACHE=$TMPDIR/vngd-gocache go build ./...`
Expected: no output. (There is nothing to test in a docs change beyond not having broken the build; read the rendered section back and confirm the nested code fence is correctly closed.)

- [ ] **Step 3: Commit and push the server branch**

```bash
git add deploy/README.md
git commit -m "docs(deploy): document TLS on the client-facing port

The existing certs section covers the voice-control channel between nodes
only, which has been read as meaning the deployed server already serves
clients over TLS. It does not.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push -u origin feat/client-port-tls
```

---

## Task 9: Client-side documentation corrections

**Back in `/Users/schiba/Projects/vanguard/vcs-srs-client` on `feat/phase-7-1-secure-transport`.**

Spec §7 lists seven documents carrying claims that the findings invalidate. Correcting them is part of the deliverable, not follow-up work.

**Files:**
- Modify: `docs/superpowers/specs/2026-05-31-vcs-client-design.md` (line 122 row; R4 line 444; R5 line 445)
- Modify: `CLAUDE.md` ("Known gap: no TLS" section; the "deployed server already has certs" claim)
- Modify: `docs/ROADMAP.md` (Phase 7 row)
- Modify: `docs/PROTO_GAPS.md` (new entry for voice-payload encryption)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing code-facing.

- [ ] **Step 1: Correct the master spec's storage table**

In `docs/superpowers/specs/2026-05-31-vcs-client-design.md`, the row at line 122 claims the session token is persisted to `%APPDATA%/VCS/session.json` at 0600. Replace the row's storage and rationale cells to state that the token is held in memory only, in `session.Session.lastToken`, and is not written to disk at any point. Note that this row described a file that was never implemented.

Also correct line 132's "Token storage is not OS-keychain-protected in v1; this is a known security gap (R4)" to say there is no token storage at all.

- [ ] **Step 2: Correct R4 and R5**

In the same file, rewrite the R4 row (line 444) to record it as **closed, not applicable**: the risk described a persisted token file that does not exist, and keychain-backed session persistence moves to Phase 2, where SSO makes re-authentication expensive enough to justify a keyring dependency.

Rewrite the R5 row (line 445) to record it as **closed by Phase 7.1**, noting that closing it required a change in `vngd-srs-server` because the client-facing gRPC listener had no TLS option at all.

- [ ] **Step 3: Correct CLAUDE.md**

Replace the "### Known gap: no TLS" section with a short section describing the post-change behaviour: the client uses the OS trust store for remote hosts, `tls_ca_file` pins a self-signed server, loopback stays plaintext, and there is no plaintext-remote path. State that the server needs a `clientTLS` block configured or the connection will fail with the "not speaking TLS" diagnosis.

Remove the claim "The deployed server already has certs" — or correct it in place to say those certificates serve the server-to-server VoiceControl channel on port 14448, not the client port.

Add Phase 7 to the status table as in-progress, with 7.1 complete and 7.2–7.4 outstanding, pointing at the decomposition spec.

- [ ] **Step 4: Update the ROADMAP**

In `docs/ROADMAP.md`, replace the Phase 7 headline-deliverables block with the four sub-phases from the decomposition spec, each with its own status marker, and mark 7.1 `[x]`. Keep the existing nine deliverables visible, redistributed under the sub-phase that owns each, so nothing is silently dropped. Preserve the detailed notification-routing paragraph verbatim under 7.2 — it carries requirements that must stay distinct.

Record under 7.1 that Phase 5's issues #1 and #2 are closed by PR #29, and that the hardcoded client-version check is resolved by server PR #215.

- [ ] **Step 5: Add the PROTO_GAPS entry**

In `docs/PROTO_GAPS.md`, add a new numbered section (following the existing format of the sections around it — "Where it shows up", "Today", "Suggested proto change"):

```markdown
## 11. Voice payload encryption

**Where it shows up:** Every UDP voice packet, on every frequency.

**Today:** None. Phase 7.1 secured the gRPC control plane with TLS, which
protects credentials, the voice secret in transit, radio state and the client
roster. The voice payload itself is unencrypted Opus over the custom UDP
protocol from Phase 5, so anyone on the path can capture and decode a
conversation.

This is a deliberate V1 decision, not an oversight. Encrypting voice needs a
key exchange the C# peer implements too, and cross-client interop is
currently blocked on `VNGD-SimpleRadioStandalone` PR #253.

**Suggested proto change:** a key-agreement step in the control plane whose
material both clients feed into a per-frequency AEAD over the UDP payload.
Requires agreement with the C# peer before either side implements it.
```

- [ ] **Step 6: Verify nothing else still claims a session.json**

Run: `grep -rn "session.json" --include='*.md' . ; echo "EXIT=$?"`
Expected: either no output with `EXIT=1`, or only matches inside the Phase 7.1 spec and this plan, which describe the finding rather than assert the file exists.

Run: `grep -rn "already has certs" --include='*.md' . ; echo "EXIT=$?"`
Expected: no match outside the Phase 7.1 spec and this plan.

- [ ] **Step 7: Commit**

```bash
git add docs/superpowers/specs/2026-05-31-vcs-client-design.md CLAUDE.md docs/ROADMAP.md docs/PROTO_GAPS.md
git commit -m "docs: correct the token-storage, TLS and Phase 7 records

The master spec described a session.json that was never implemented, so R4
described a risk to a file that does not exist; it closes as not applicable
and keychain persistence moves to Phase 2. R5 closes, noting it needed a
server change. CLAUDE.md's claim that the deployed server already has certs
was about the VoiceControl channel on a different port.

ROADMAP's Phase 7 row splits into the four sub-phases. Voice-payload
encryption is recorded in PROTO_GAPS as a known V1 limitation.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Manual verification checklist

Every claim in the spec about handshake behaviour is designed from reading source, not measured. Per the umbrella spec, this checklist is not deferred debt — it is the first one that should actually be run, because reaching a real server is what the five queued checklists have all been waiting on.

**Files:**
- Create: `docs/superpowers/plans/2026-09-28-phase-7-1-manual-verification.md`

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code-facing.

- [ ] **Step 1: Write the checklist**

Create the file with a numbered checklist covering, each with an explicit expected observation and a space to record what actually happened:

1. **Server plaintext, unchanged** — start the server with no `clientTLS` block. Expect the startup WARN naming the plaintext client port; expect a localhost client to connect exactly as before.
2. **Server half-configured** — set `certificateFile` only. Expect startup to fail naming `privateKeyFile`, and expect the server **not** to come up plaintext.
3. **Server generates its pair** — configure both paths at locations that do not exist. Expect both files created, mode 0600, and the log line reporting TLS with the configured `serverName`.
4. **Restart reuses the pair** — restart the server. Expect the certificate bytes unchanged (a fresh one would break every pinning client).
5. **Client pins a self-signed server** — copy `srs-cert.pem` to the client machine, set `tls_ca_file`, connect by the hostname in `serverName`. Expect a successful guest login and a CONNECTED pill.
6. **Client pins by bare IP** — set `serverName` to the IP before first generation, connect by IP. Expect success. If it fails, record the exact error: this is the case the spec flags as needing an IP SAN.
7. **Client with the wrong pin** — point `tls_ca_file` at an unrelated certificate. Expect the "not issued by the authority in tls_ca_file" message, naming the file.
8. **Client against a plaintext server** — configure `tls_ca_file` (or use a remote host) against a server with no `clientTLS`. Expect the "is not speaking TLS ... no clientTLS block configured" message.
9. **Remote host with no pin and no real certificate** — expect the "no system root trusts ... point tls_ca_file at its certificate" message.
10. **Loopback unchanged** — no `tls_ca_file`, connect to `localhost:5002` plaintext. Expect success, proving the development path did not regress.
11. **The deployed server over TLS** — the definition-of-done item. Connect to the real deployment with a CA-signed certificate and no `tls_ca_file`. Expect success with zero client configuration.
12. **Voice still works over the TLS control plane** — after 11, confirm the voice secret arrives and a UDP session establishes. TLS changed how the secret is delivered; this proves the delivery still works.

For each item record: date, platform, server version, client build, observed result, and any deviation from the expected text. Note at the top that items 1–4 run on the server host and 5–12 on a client machine.

- [ ] **Step 2: Run the full client suite one final time**

Run: `GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./... && GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...`
Expected: no output.

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./... 2>&1 | tail -40`
Expected: all packages `ok` **except** `internal/voice`, which fails inside the sandbox with "listen udp: operation not permitted".

Run `internal/voice` separately with `dangerouslyDisableSandbox: true`:
`GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/voice/...`
Expected: PASS.

Run: `(cd frontend && npx tsc --noEmit)`
Expected: no output. This sub-phase touches no frontend code, so this is a pure regression gate. If it reports missing methods on the App bindings, that is branch-switch staleness — regenerate with `wails3 generate bindings -ts -f "-tags purego" -clean=true` and re-run.

Run: `(cd frontend && npx vitest run 2>&1 | tail -20)`
Expected: all tests pass.

- [ ] **Step 3: Commit and push**

```bash
git add docs/superpowers/plans/2026-09-28-phase-7-1-manual-verification.md
git commit -m "docs(phase-7-1): add the manual verification checklist

Twelve items, four on the server host and eight on a client. Item 11 --
connecting to the deployed server over TLS -- is a definition-of-done
item, not follow-up work: reaching a real server is what the five queued
checklists from Phases 3 through 6 have all been waiting on.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push -u origin feat/phase-7-1-secure-transport
```

---

## Completion

Before claiming the sub-phase done, confirm every item in the spec's §10 definition of done. Items 1–8 are verifiable from the automated suite and the code; **item 9 requires a human** and cannot be self-certified. Report it as outstanding rather than marking it complete.

Then use `superpowers:requesting-code-review` for a whole-branch review of each repo's branch separately. Only a clean whole-branch verdict unlocks proposing to finish; rerun it after every fix wave.

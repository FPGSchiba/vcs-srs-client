// Package testdata brings up a REAL, headless vcs-srs-server for Task 12's
// integration tests (internal/voice/integration_test.go).
//
// It lives under a directory literally named "testdata" on purpose: `go
// build ./...`, `go vet ./...` and the `...` expansion generally all skip
// directories named "testdata" (see `go help packages`), so this package
// never needs its own build tag to stay out of the ordinary build/vet
// sweep. It is still an ordinary importable Go package -- nothing here is a
// "_test.go" file -- so integration_test.go can import it directly, exactly
// as it would any other internal package.
//
// Do not grow this into a second server implementation. Its only job is to
// exec the real binary and report when it is ready; every protocol decision
// belongs to the real server, which is the entire point of Task 12 (see the
// phase design doc §11.1 and the package doc on ../testserver_test.go).
package testdata

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CoalitionName and CoalitionPassword are the one coalition the fixture
// configures. The password is PLAINTEXT in config.yaml -- the server
// bcrypt-compares the client's hash against this stored value, it is not
// itself a bcrypt hash (see CLAUDE.md's "Password handling looks odd but is
// correct" note and utils/general.go's CheckPasswordHash on the server).
const (
	CoalitionName     = "Blue"
	CoalitionPassword = "task12-integration-password"

	// TestFrequencyMHz and GlobalFrequencyMHz are the two frequencies the
	// fixture's ServerSettings advertises, matching what
	// TestIntegrationTestFrequencyLoopback and any global-channel case need.
	TestFrequencyMHz   = float32(100.000)
	GlobalFrequencyMHz = float32(200.000)

	// readyTimeout bounds how long Start waits for both listeners to report
	// ready before failing the test outright, so a hung or crashed server
	// fails fast with its log attached rather than hanging the whole suite.
	readyTimeout = 20 * time.Second
)

// Server describes one running vcs-srs-server fixture instance: everything
// the real client stack needs to dial it.
type Server struct {
	// ControlAddr is the "host:port" gRPC control-plane target -- what
	// session.Session.Connect expects as its serverURL argument.
	ControlAddr string

	// VoiceHost and VoicePort are the UDP voice endpoint. The standalone
	// server always returns "" for coalition_voice_addr (see the Phase 5
	// design doc §6 -- that field is only populated by a distributed voice
	// node's registry entry), so tests must supply these as
	// voice.Sources.ConfigHost/ConfigPort rather than relying on
	// SyncClient's response.
	VoiceHost string
	VoicePort int

	Coalition string
	Password  string

	logPath string
}

// RequireServer reports the real server binary's path, or skips the test
// cleanly if VCS_SERVER_BIN is unset. Every integration test calls this
// first so `go test ./...` stays green on a machine with no server.
func RequireServer(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("VCS_SERVER_BIN")
	if bin == "" {
		t.Skip("VCS_SERVER_BIN not set; skipping real-server integration tests")
	}
	return bin
}

// Start execs a real vcs-srs-server (headless, standalone, autostart)
// against a freshly generated config in t.TempDir(), waits for both its
// control and voice listeners to come up, and registers cleanup that kills
// the process and -- on test failure -- dumps its combined stdout/stderr log.
//
// Every port is allocated dynamically (bound briefly as TCP, then released)
// rather than hardcoded: a hardcoded port collides with any other instance
// left running on the same machine -- including, concretely, a previous
// manual verification run left resident on this very box while this fixture
// was being built -- and CI runners execute jobs from more than one
// workflow on the same host.
func Start(t *testing.T) *Server {
	t.Helper()
	bin := RequireServer(t)
	dir := t.TempDir()

	genKeys(t, dir)

	httpPort := freePort(t)
	voicePort := freePort(t)
	controlPort := freePort(t)

	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig(t, cfgPath, dir, httpPort, voicePort, controlPort)

	bannedPath := filepath.Join(dir, "banned_clients.json")
	if err := os.WriteFile(bannedPath, []byte("[]"), 0o644); err != nil {
		t.Fatalf("testdata: write banned clients file: %v", err)
	}

	logDir := filepath.Join(dir, "log")
	logPath := filepath.Join(dir, "server-output.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("testdata: create server log file: %v", err)
	}

	cmd := exec.Command(bin,
		"--mode", "standalone",
		"--autostart",
		"--config", cfgPath,
		"--banned", bannedPath,
		"--log-folder", logDir,
	)
	// Stdout and Stderr are the SAME writer, which os/exec documents as safe
	// for concurrent use by the two streams -- a separate io.Writer per
	// stream would need its own synchronization instead.
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("testdata: start %s: %v", bin, err)
	}

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("vcs-srs-server output (pid %d):\n%s", cmd.Process.Pid, data)
		}
		logFile.Close()
	})

	waitReady(t, logPath)

	return &Server{
		ControlAddr: fmt.Sprintf("127.0.0.1:%d", controlPort),
		VoiceHost:   "127.0.0.1",
		VoicePort:   voicePort,
		Coalition:   CoalitionName,
		Password:    CoalitionPassword,
		logPath:     logPath,
	}
}

// DumpLog reports the server's combined stdout/stderr so far. Tests use this
// to attach server-side context to an assertion failure that is not itself a
// startup failure (Start's own t.Cleanup already covers that case).
func (s *Server) DumpLog(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		t.Logf("vcs-srs-server: could not read log: %v", err)
		return
	}
	t.Logf("vcs-srs-server output:\n%s", data)
}

// freePort asks the OS for an unused TCP port by binding to port 0 and
// immediately releasing it. There is an inherent, unavoidable gap between
// releasing the port here and the server binding it a moment later -- the
// same gap every "find a free port" helper in the Go ecosystem accepts --
// but it is far narrower than the collision a hardcoded port guarantees
// against a leftover process.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("testdata: allocate free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// genKeys writes an ECDSA P-256 keypair PEM-encoded exactly the way the
// server's utils/auth.go decode() expects: private key via
// x509.MarshalECPrivateKey in a "PRIVATE KEY" block, public key via
// x509.MarshalPKIXPublicKey in a "PUBLIC KEY" block.
func genKeys(t *testing.T, dir string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("testdata: generate ECDSA key: %v", err)
	}

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("testdata: marshal EC private key: %v", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), privPEM, 0o600); err != nil {
		t.Fatalf("testdata: write private key: %v", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("testdata: marshal PKIX public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	if err := os.WriteFile(filepath.Join(dir, "pub.pem"), pubPEM, 0o644); err != nil {
		t.Fatalf("testdata: write public key: %v", err)
	}
}

// yamlSingleQuote renders s as a single-quoted YAML scalar. Single quotes are
// used (not double) specifically so a Windows temp path's backslashes are
// carried through LITERALLY -- YAML only special-cases a doubled single
// quote inside a single-quoted scalar, unlike a double-quoted one where a
// backslash is an escape introducer.
func yamlSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// writeConfig renders config.yaml. Structurally identical to
// .superpowers/sdd/verified-server-config.yaml (hand-verified to bring up
// both listeners with zero errors) with three differences: dynamically
// allocated ports, this fixture's own generated key paths, and a
// process-unique coalition password.
func writeConfig(t *testing.T, path, dir string, httpPort, voicePort, controlPort int) {
	t.Helper()
	cfg := fmt.Sprintf(`servers:
  http:
    host: 127.0.0.1
    port: %d
  voice:
    host: 127.0.0.1
    port: %d
  control:
    host: 127.0.0.1
    port: %d
coalitions:
  - name: %s
    password: %s
    color: "#3b82f6"
frequencies:
  testFrequencies: [%.3f]
  globalFrequencies: [%.3f]
general:
  maxRadiosPerUser: 20
security:
  plugins: []
  enablePluginAuth: false
  enableGuestAuth: true
  token:
    expiration: 28800
    privateKeyFile: %s
    publicKeyFile: %s
    issuer: https://vcs.vngd.net
    subject: vcs.vngd.net
voiceControl:
  port: 14448
  remoteHost: localhost
  listenHost: 127.0.0.1
  certificateFile: ""
  privateKeyFile: ""
  publicAddr: ""
  region: ""
api:
  key: ""
`,
		httpPort, voicePort, controlPort,
		CoalitionName, CoalitionPassword,
		TestFrequencyMHz, GlobalFrequencyMHz,
		yamlSingleQuote(filepath.Join(dir, "key.pem")),
		yamlSingleQuote(filepath.Join(dir, "pub.pem")),
	)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("testdata: write config.yaml: %v", err)
	}
}

// waitReady polls the server's combined log for the two lines that mark both
// listeners up (confirmed verbatim against a real run: "Starting VCS gRPC
// server" and "Voice server started"), failing fast -- with the log attached
// -- on an explicit startup error rather than waiting out the full timeout.
func waitReady(t *testing.T, logPath string) {
	t.Helper()
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(logPath)
		out := string(data)
		if strings.Contains(out, "Starting VCS gRPC server") && strings.Contains(out, "Voice server started") {
			return
		}
		if strings.Contains(out, "level=ERROR") {
			t.Fatalf("vcs-srs-server reported an error before becoming ready:\n%s", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
	data, _ := os.ReadFile(logPath)
	t.Fatalf("vcs-srs-server did not report both listeners ready within %s; log:\n%s", readyTimeout, data)
}

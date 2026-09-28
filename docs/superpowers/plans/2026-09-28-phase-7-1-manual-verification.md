# Phase 7.1 — Manual verification: secure transport

## Why this checklist matters more than usual

Phases 3, 3.5, 4, 5 and 6 of this project are all code-complete and NONE has
ever been field-verified. Five manual checklists sit unrun in
`docs/superpowers/plans/`. Nothing in the voice path has ever been heard by
a human, and nobody has watched this client lose a connection to a real
server. Every claim in the 7.1 spec about handshake behaviour, error text
and failure modes is designed from reading source, not measured.

This checklist is therefore NOT deferred verification debt. It is the first
one that should actually be run, because reaching a real remote server is
the thing all five queued checklists have been waiting on —
`internal/session/dial.go` previously fail-closed on any non-loopback host,
so the client could only ever reach a server on the same machine.

## How to use this document

- **Items 1–4 run on the server host.** They exercise
  `vngd-srs-server/control/server.go`'s `clientTransportCredentials` and the
  shared `LoadOrGenerateKeyPair` helper, with no client involved.
- **Items 5–12 run on a client machine** reaching that server (or, for item
  11, the real deployment) over the network.
- For **every** item, record:
  - **Date**
  - **Platform** (OS/arch of the machine running that step)
  - **Server version** — the `vngd-srs-server` git commit SHA under test
    (`git rev-parse HEAD`); there is no `--version` flag
  - **Client build** — the `vcs-srs-client` git commit SHA under test
    (`git rev-parse HEAD`); there is no `--version` flag
  - **Observed result**
  - **Any deviation from the expected text below** — copy the actual string
    verbatim if it differs at all from what is quoted here

**A vague "worked" / "didn't work" does not satisfy this checklist.** The
expected observations below are the literal strings the code emits; a
result should either match them or quote exactly what came out instead.

## SFX pack caveat — read before testing

The nine-slot SFX sample pack (`docs/ROADMAP.md` Phase 4 entry) has never
been delivered. Connection sounds are **silent** on this build. If you hear
nothing when a radio or connection event fires, that is the known missing
WAV pack, not a TLS failure. Do not record silence as a failed handshake —
judge success by the CONNECTED pill / login result / logged text, not by
audio.

---

## Server-host items (1–4)

### Item 1 — Server plaintext, unchanged

**Setup:** Start the server with no `clientTLS` block in its config YAML
(the stock `servers.control` block, nothing under `clientTLS:` at all).
Connect a client on the same host at `localhost:<servers.control.port>`
(`14446` in `dev/config/control.yaml`; `5002` in `example.config.yaml` —
use whatever `servers.control.port` your config actually sets).

**Expected:**
- Startup log line (WARN level), verbatim from
  `clientTransportCredentials` in `vngd-srs-server/control/server.go`:

  > `client-facing gRPC port is PLAINTEXT: no clientTLS block configured, so client credentials cross the network in the clear`

- A localhost client connects and completes guest login exactly as before
  this sub-phase — no behavior change on the plaintext path.

**Actual result:**
_(record here)_

---

### Item 2 — Server half-configured

**Setup:** Set only `clientTLS.certificateFile` in the server config (leave
`privateKeyFile` empty), then start the server. Repeat the inverse (only
`privateKeyFile` set, `certificateFile` empty) as a second run if time
allows.

**Expected:**
- The client-facing gRPC port does not come up: the server process itself
  keeps running (it does not exit or restart) and the admin HTTP/GraphQL
  port is unaffected. The error names the missing field, verbatim from
  `clientTransportCredentials`:

  > `clientTLS.certificateFile is set but clientTLS.privateKeyFile is empty: configure both or neither`

  (or, for the inverse case:)

  > `clientTLS.privateKeyFile is set but clientTLS.certificateFile is empty: configure both or neither`

- The client port must **not** come up plaintext in this state — there is
  no fallback. Confirm no client can connect at all (connection refused/no
  listener), not that it silently accepted plaintext.

**Actual result:**
_(record here)_

---

### Item 3 — Server generates its pair

**Setup:** Configure `clientTLS.certificateFile` and `clientTLS.privateKeyFile`
at paths that do not yet exist on disk. Set `clientTLS.serverName` to a
recognizable value (e.g. your test hostname). Start the server.

**Expected:**
- Both files are created at the configured paths.
- Both files have mode **0600** (`ls -l` / `stat` each file and confirm
  `-rw-------`) — enforced by `os.WriteFile(..., 0600)` in
  `voiceontrol.LoadOrGenerateKeyPair` (`vngd-srs-server/voiceontrol/utils.go`).
- Startup log line (INFO level), verbatim from `clientTransportCredentials`:

  > `client-facing gRPC port is TLS` with fields `certificateFile=<path>` `serverName=<configured serverName>`

**Actual result:**
_(record here)_

---

### Item 4 — Restart reuses the pair

**Setup:** With item 3's server stopped (not the files deleted), record a
hash of the certificate file (e.g. `sha256sum <certificateFile>`), then
restart the server against the same config.

**Expected:**
- `LoadOrGenerateKeyPair` only generates when the private key file is
  **absent** (`os.Stat` / `os.IsNotExist` check in
  `vngd-srs-server/voiceontrol/utils.go`); on restart it takes the
  `tls.LoadX509KeyPair` branch instead.
- The certificate file's bytes/hash are **identical** before and after the
  restart. A fresh certificate here would break every client that pinned
  the old one via `tls_ca_file`.

**Actual result:**
_(record here)_

---

## Client-machine items (5–12)

### Item 5 — Client pins a self-signed server

**Setup:** Copy the server's `srs-cert.pem` (from item 3) to the client
machine. In the client's `config.toml`, set `tls_ca_file` to that file's
local path, and `server_url` to `<serverName from item 3>:<port>` — i.e.
connect by the **hostname** the certificate's SAN was generated for, not a
bare IP. Launch the client and perform guest login.

**Expected:**
- Guest login succeeds (`Welcome.tsx` → `App.Connect` →
  `session.Connect` → `InitAuth` → `GuestLogin` → `SyncClient` →
  `SubscribeToUpdates` completes without error).
- The status bar shows the CONNECTED pill (see `internal/connhealth` /
  Phase 6 ConnBanner).

**Actual result:**
_(record here)_

---

### Item 6 — Client pins by bare IP

**Setup:** Before first generation of the server's TLS pair, set
`clientTLS.serverName` to the server's bare IP address (e.g. `10.0.0.5`), so
`generateSelfSignedCert` adds it as an `IPAddresses` SAN
(`vngd-srs-server/voiceontrol/utils.go`: `net.ParseIP(host)` succeeds for an
IP, so it is added via `ipAddresses = append(...)` rather than `dnsNames`).
On the client, set `tls_ca_file` to the resulting `srs-cert.pem` and
`server_url` to that same IP:port. Connect.

**Expected:**
- Success — the certificate carries an IP SAN, so gRPC's hostname
  verification against the dialed authority (the bare IP) should pass.
- **If it fails, record the exact error text verbatim.** This is the case
  the spec (§5.1) flags as needing an IP SAN; a failure here would mean the
  SAN either wasn't generated correctly or gRPC's authority derivation
  doesn't match it as expected. Do not paraphrase the error — the literal
  gRPC/TLS message matters for diagnosing which side is wrong.

**Actual result:**
_(record here)_

---

### Item 7 — Client with the wrong pin

**Setup:** On the client, set `tls_ca_file` to a certificate that is
**unrelated** to the server's actual certificate (e.g. a different
self-signed cert generated for another purpose). Connect to the TLS server
from item 3/5.

**Expected:** an error containing this phrase, verbatim from
`explainDialError` in `internal/session/dial.go`, naming the configured
file:

> `<server_url> presented a certificate not issued by the authority in tls_ca_file "<path-to-the-wrong-file>": <underlying gRPC/TLS error>`

**Actual result:**
_(record here)_

---

### Item 8 — Client against a plaintext server

**Setup:** Point the client at a server running with **no** `clientTLS`
block (item 1's server), but configure the client with `tls_ca_file` set (or
otherwise force the TLS branch — e.g. connect to a non-loopback host with no
`tls_ca_file`, which also takes the TLS-with-system-roots branch).

**Expected:** an error containing this phrase, verbatim from
`explainDialError`:

> `<server_url> is not speaking TLS -- the server most likely has no clientTLS block configured: <underlying gRPC/TLS error>`

(This fires on the `"first record does not look like a TLS handshake"`
substring match against the raw gRPC transport error.)

**Actual result:**
_(record here)_

---

### Item 9 — Remote host with no pin and no real certificate

**Setup:** Connect to a **non-loopback** host with `tls_ca_file` left empty,
where that host is serving a self-signed certificate not trusted by the OS
(i.e. no real CA-signed cert, and the client isn't pinning it).

**Expected:** an error containing this phrase, verbatim from
`explainDialError`:

> `<server_url> presented a certificate that no system root trusts -- if this is a self-hosted server, point tls_ca_file at its certificate: <underlying gRPC/TLS error>`

(Same code branch as item 7 — matched on `"certificate signed by unknown authority"` or `"failed to verify certificate"` — but `caFile == ""` selects this wording instead of the tls_ca_file-naming one.)

**Actual result:**
_(record here)_

---

### Item 10 — Loopback unchanged

**Setup:** No `tls_ca_file` configured. Connect to
`localhost:<servers.control.port>` (matches `isLocal(host)` in
`internal/session/dial.go`: `localhost`, `127.0.0.1`, `::1`, or any
`127.0.0.0/8` address). Use whatever port your server config actually
listens on for `servers.control` (`14446` in the repo's `dev/config/control.yaml`;
`example.config.yaml` shows `5002` — confirm the value in the config you are
actually running against before assuming either number).

**Expected:**
- `transportCredentials` takes the `insecure.NewCredentials()` branch — no
  TLS handshake at all.
- Guest login succeeds exactly as it did before this sub-phase. This proves
  the development path did not regress.

**Actual result:**
_(record here)_

---

### Item 11 — The deployed server over TLS (definition-of-done item)

**Setup:** Connect the client to the real deployed server, which holds a
CA-signed (not self-signed) certificate, with **zero** client-side TLS
configuration — `tls_ca_file` left empty, `server_url` pointed at the
deployment's real hostname.

**Expected:**
- `transportCredentials` takes the "otherwise" branch —
  `credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})` against the
  OS trust store, no pinning.
- Guest login succeeds with **zero client configuration** beyond the server
  address the user already had to type.

**Actual result:**
_(record here)_

**This item requires a human to have actually done it.** Per the phase
spec §10 definition-of-done item 9, this cannot be self-certified by an
agent or by reading source — it must be performed and observed by a person.

---

### Item 12 — Voice still works over the TLS control plane

**Setup:** Immediately after item 11 succeeds, proceed to join a radio /
frequency as normal and attempt to transmit/receive.

**Expected:**
- The voice secret is delivered to the client over the now-TLS control
  connection (this is the thing TLS changed — the delivery mechanism, not
  the voice UDP payload itself, which per spec §9 remains unencrypted).
- A UDP voice session establishes: PTT transmits, and (with a second client
  or a known-good peer) audio is receivable. Remember the SFX-pack caveat
  above — judge by actual voice audio and radio state, not connection
  chimes, which are silent.

**Actual result:**
_(record here)_

---

## Summary table (fill in after running all items)

| # | Result (PASS/FAIL/BLOCKED) | Deviation from expected text | Tester | Date |
|---|---|---|---|---|
| 1 |  |  |  |  |
| 2 |  |  |  |  |
| 3 |  |  |  |  |
| 4 |  |  |  |  |
| 5 |  |  |  |  |
| 6 |  |  |  |  |
| 7 |  |  |  |  |
| 8 |  |  |  |  |
| 9 |  |  |  |  |
| 10 |  |  |  |  |
| 11 |  |  |  |  |
| 12 |  |  |  |  |

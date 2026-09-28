# Phase 7.1 — Secure transport (TLS on the control plane)

**Date:** 2026-09-28
**Status:** approved, not yet implemented
**Branch:** `feat/phase-7-1-secure-transport`
**Closes:** master-spec risk **R5**. Resolves **R4** as not-applicable (see §7).
**Repos touched:** `vcs-srs-client` *and* `vngd-srs-server` — two PRs.

---

## 1. Why this is a sub-phase, and why it is first

The ROADMAP's Phase 7 row lists nine deliverables spanning at least four
independent subsystems. That is not one phase. It decomposes as:

| Sub-phase | Contents | Rationale for the grouping |
|---|---|---|
| **7.1** (this doc) | TLS on the control plane, client + server; R4 resolution | The only item that unblocks field verification; both halves are one wire change |
| **7.2** | Notifications popout + routing hotkey/joystick registration failures into it | Smallest coherent unit, and a *dependency* of the later popouts rather than a peer — every popout will want to raise notifications |
| **7.3** | Radio profiles + transmission history | Both are "JSON file under `AppDataDir` behind a main-window nav screen" |
| **7.4** | Ship Mode, Fleet C2, Messages popouts | ~1,800 lines of prototype JSX, all local-only, no server data. May split again at plan time |

Each sub-phase gets its own spec, plan and execution cycle.

7.1 goes first because Phases 3, 3.5, 4, 5 and 6 are all code-complete and
none has been field-verified — five manual checklists sit unrun in
`docs/superpowers/plans/`. Nothing in the voice path has ever been heard by
a human. `internal/session/dial.go` fails closed on any non-localhost host,
so the client can only reach a server on the same machine. Making remote
verification *possible* is worth more than any feature in 7.2–7.4.

---

## 2. Findings that invalidate parts of the existing documentation

These were established by reading source in both repos, and each contradicts
something currently written down. They are recorded here because the
documentation corrections in §7 depend on them.

### 2.1 R5 cannot be closed from the client repo alone

`vngd-srs-server/control/server.go:97` constructs `clientGrpcServer` — the
gRPC server that registers `SRSService` and `AuthService`, i.e. the port this
client dials — with **no `grpc.Creds(...)` option**. It is plaintext
unconditionally, with no configuration option to change that.

Every TLS artefact in that repository belongs to the *server-to-server*
VoiceControl channel on port 14448: `initControlServer` (`control/server.go`,
from line 147), the `voicecontrol-*.pem` pair, and all fourteen certificate
mentions in `deploy/README.md`. The `srs-cert.pem` / `srs-private-key.pem`
files at the server repo root are gitignored (`.gitignore:13`, `*.pem`) and
referenced by zero Go code and zero configuration.

CLAUDE.md's claim that "the deployed server already has certs" is true about
the wrong channel. A client taught to speak TLS against a server that cannot
listen for it changes nothing, which is why this sub-phase spans both repos.

### 2.2 R4 rests on a false premise

The master spec (`2026-05-31-vcs-client-design.md`, line 122) states the session
token is persisted to `%APPDATA%/VCS/session.json` at mode 0600, and R4
(line 444) tickets migrating that file to the OS keychain in Phase 7.

No such file exists. `grep -rn "session.json" --include='*.go'` over the
client repo returns nothing. The token is `lastToken string`, an in-memory
field at `internal/session/session.go:52`, read only by `Reconnect`
(line 312) and never written to disk.

There is therefore nothing to migrate and no on-disk secret to protect.
Adding keychain storage would *add* token persistence — a new "stay signed
in" feature — rather than close a security gap. See §7 for the resolution.

### 2.3 Two recorded blockers are already fixed

PR #29 (`8497c14`, on `main`) added `buf generate` to `release.yml` and a
guard that hard-fails the release when any binary reports `CGO_ENABLED=0`
(`release.yml:103-108`). Phase 5's issues #1 (Windows ships without audio)
and #2 (`release.yml` never runs `buf generate`) are closed. The SFX sample
pack and the C# peer interop blocker (`VNGD-SimpleRadioStandalone` PR #253
sends no voice secret in its HELLO) still stand.

---

## 3. Trust model

**System roots by default, with an optional pinned CA.**

The public deployment needs CA-signed certificates and OS trust-store
verification: zero client configuration, zero certificate distribution, and
no opportunity for a user to be talked into trusting the wrong `.pem`. But a
Star Citizen org self-hosting on a LAN has no public DNS and no route to a
CA, and pinning serves that case for roughly twenty lines more code.

Pinning *only* (mirroring what the VoiceControl channel does) was rejected:
it would require every user of the public deployment to fetch a certificate
out of band, and a certificate fetched over an insecure channel reopens
precisely the man-in-the-middle exposure R5 exists to close.

---

## 4. Server change (`vngd-srs-server`)

### 4.1 Configuration

`SettingsState` gains a **top-level** `clientTLS` block, sibling to the
`voiceControl` block it deliberately mirrors (`state/settings.go:97`):

```yaml
clientTLS:
  certificateFile: /certs/srs-cert.pem
  privateKeyFile:  /certs/srs-private-key.pem
  serverName:      vcs.vngd.net
```

**It is top-level rather than nested under `servers.control`, and that is a
correctness requirement, not a style preference.** `app/settings.go:44`
`SaveServerSettings` assigns `a.SettingsState.Servers = *newSettings` — a
wholesale replacement — and `graphql/schema.resolvers.go:76` rebuilds
`ServerSettings` from GraphQL input carrying only host and port. TLS nested
inside `Servers` would therefore be erased by any admin-UI settings save and
persisted as plaintext by the following `Save()`, silently downgrading the
server on its next restart. A top-level block is outside that write path, and
needs no GraphQL schema change.

This corrects the `servers.control.tls` shape proposed when this spec was
first approved; the discovery came from tracing the admin write path during
planning.

### 4.2 Behaviour

In `control/server.go`, `clientGrpcServer` gains `grpc.Creds(...)` when both
certificate and key paths are configured. It reuses the existing
`voiceontrol.LoadOrGenerateKeyPair` (`voiceontrol/utils.go:19`), which
generates a self-signed pair at the configured paths when they do not exist
and loads them when they do. `serverName` is passed as `extraHosts` so the
generated certificate carries the right SANs.

This mirrors the workflow `deploy/README.md` already documents for voice
nodes: the server generates on first run, and the operator copies the public
certificate to the clients that need to pin it.

With no TLS block configured the server stays plaintext, so every existing
local development setup keeps working unchanged. It logs a startup WARN that
names the consequence — credentials in cleartext on the client port — rather
than staying silent about it.

### 4.3 Accepted operational sharp edge

Because the keypair is generated rather than demanded, a misconfigured
public deployment can come up holding a self-signed certificate and fail
every client at once. This is accepted: it fails loudly and identically for
everyone, at startup, with the server's own WARN plus a certificate-
verification error on each client. The alternative — refusing to start —
trades a loud, diagnosable failure for a hard outage, and does nothing that
the log line does not already do.

### 4.4 Cleanup

The stray `srs-cert.pem` / `srs-private-key.pem` at the server repo root
become the documented default paths in `example.config.yaml` rather than
unreferenced litter.

---

## 5. Client change (`vcs-srs-client`)

### 5.1 Dialer

`insecureDialer` (`internal/session/dial.go:50`, called from
`internal/session/session.go:226`) becomes `dialerFor(serverURL, caFile)`
with three deterministic branches:

| Condition | Transport |
|---|---|
| `tls_ca_file` is set | TLS, trusting only that pool — any host |
| else host is `localhost` / `127.0.0.0/8` / `::1` | insecure (unchanged from today) |
| else | TLS, system roots |

Two things this deliberately does not have:

- **No plaintext-remote escape hatch.** Today's fail-closed rule inverts
  rather than relaxes: non-localhost is currently refused outright, and
  afterwards it requires TLS.
- **No fall back to plaintext when the handshake fails.** A fallback is a
  downgrade attack with extra steps.

The pinned branch is not gated on the host, which is what makes it double as
the local-TLS development and test path. That saves a second configuration
key.

gRPC derives the TLS ServerName from the dialed authority, so pinning
against a bare IP address requires the certificate to carry an IP SAN.
`generateSelfSignedCert(extraHosts...)` already emits those, so this needs
no additional configuration key — but it is a real constraint and belongs in
the operator documentation.

`keepaliveOption()` and the `clientKeepaliveTime` reasoning are unchanged;
they are orthogonal to transport credentials.

### 5.2 Configuration surface

`config.toml` gains one key, `tls_ca_file`, beside `server_url`.

No settings UI. The server address is typed on the Welcome form and never
written to config — `internal/app/bindings.go:31` documents exactly this, as
the F1 fix from the Phase 6 whole-branch review — so there is nothing for a
toggle to attach to. Deriving the transport from the address is what keeps
this change UI-free.

---

## 6. Error handling

The failure that will actually occur in the field is a TLS client meeting a
plaintext server: an operator upgrades the client before the server, or
forgets the `tls:` block. gRPC surfaces this as a generic transport error,
and the user sees `connection error: desc = "transport: ..."`.

Both of the two diagnosable cases are wrapped with distinguishable,
actionable text:

- **Server is not speaking TLS** — name the likely cause (server has no
  `tls:` block configured) and the remedy.
- **Certificate not trusted** — distinguish "not signed by a trusted CA"
  from "hostname does not match", since the remedies differ (obtain a real
  certificate vs. fix `serverName` / connect by hostname rather than IP).

These reach the user through the existing `internal/connhealth` surface and
the ConnBanner delivered in Phase 6; this sub-phase adds no new error
presentation path.

---

## 7. R4 resolution and documentation corrections

**R4 closes as not-applicable.** Per §2.2 there is no on-disk token, so
there is nothing to migrate. Keychain-backed session persistence moves to
Phase 2, where plugin SSO makes re-authentication expensive enough to
justify a keyring dependency; today it would save re-typing one coalition
password within an 8-hour token expiry. No keyring dependency is added.

Documents corrected as part of this sub-phase:

| Document | Correction |
|---|---|
| `2026-05-31-vcs-client-design.md` line 122 | Token is memory-only; `session.json` does not exist |
| `2026-05-31-vcs-client-design.md` R4 (line 444) | Closed, not-applicable; keychain persistence deferred to Phase 2 |
| `2026-05-31-vcs-client-design.md` R5 (line 445) | Closed by this sub-phase; note it required a server change |
| `CLAUDE.md` "Known gap: no TLS" | Rewritten for the post-TLS state |
| `CLAUDE.md` "the deployed server already has certs" | Corrected — those certs serve the VoiceControl channel |
| `docs/ROADMAP.md` Phase 7 row | Split into 7.1–7.4 per §1 |
| `vngd-srs-server/deploy/README.md` | Client-port TLS setup, and the IP-SAN constraint from §5.1 |
| `vngd-srs-server/example.config.yaml` | The top-level `clientTLS` block (not `servers.control.tls` — see §4.1) |
| `docs/PROTO_GAPS.md` | New entry: voice-payload encryption, recorded as a known V1 limitation (§9) |

---

## 8. Testing

The governing constraint is that the sandbox blocks `bind(2)`, which is why
`internal/voice`'s tests must run with `dangerouslyDisableSandbox`.

**TLS over `bufconn` avoids that entirely.** TLS is a byte stream over the
connection, so `credentials.NewTLS` layered on a `bufconn` dialer with an
in-test generated certificate exercises the real handshake with no listening
socket. This suite runs sandboxed. `internal/grpctest` gains a TLS-enabled
variant of its `Fake` rather than a second harness.

| Level | Coverage |
|---|---|
| Unit | `dialerFor` branch selection, table-driven over host × `caFile`. Pure, no network |
| Integration | Trusted certificate succeeds; untrusted certificate is refused; TLS client against plaintext server produces the §6 wrapped error |
| Server-side | Table test over the settings → credentials decision, including the no-TLS-block plaintext path and its WARN |

Every Go invocation carries `-tags purego` with `GOCACHE=$TMPDIR/vcs-gocache`.
Frontend typechecking is unaffected — this sub-phase touches no frontend
code — but `(cd frontend && npx tsc --noEmit)` still runs as a regression
gate.

---

## 9. Out of scope

**Voice UDP stays unencrypted.** This sub-phase secures the control plane:
credentials, the voice secret in transit, radio state and the client roster.
It does not make a conversation confidential. Anyone on the path can still
capture and decode voice traffic, because the Opus payload travels in the
clear over the custom UDP protocol from Phase 5.

This is a deliberate V1 decision, not an oversight, and is documented rather
than deferred to a numbered phase. Encrypting voice would require a key
exchange the C# peer also implements, and cross-client interop is already
blocked (§2.3). It is recorded in `docs/PROTO_GAPS.md` alongside the other
wire-level candidates.

Also out of scope: the REST/GraphQL surface on the HTTP port (Phase 8), and
the VoiceControl channel, which already has TLS.

---

## 10. Verification

Nobody has ever pointed this client at a remote server. Every claim in this
document about handshake behaviour, error text and failure modes is designed
from reading source, not measured — the same footing Phase 6's timing claims
are on.

So the manual checklist for this sub-phase is not deferred verification
debt. It is the first checklist that should actually be run, and
"connected to the deployed server over TLS, by a human, and observed" is
part of 7.1 rather than a follow-up. The five checklists already queued in
`docs/superpowers/plans/` become runnable for the first time once it passes,
because reaching a real server is what every one of them has been waiting
on.

### Definition of done

1. Server accepts TLS on the client port when configured, and stays
   plaintext with a WARN when not.
2. Client reaches a remote host over TLS with system roots.
3. Client reaches a pinned self-signed server, including by IP with an IP SAN.
4. Client refuses an untrusted certificate, and refuses plaintext on a
   remote host.
5. Localhost plaintext development still works unchanged.
6. Both §6 error cases produce their distinguishable wrapped text.
7. Full suite green in both repos: `go build` / `go vet` / `go test -race`
   with `-tags purego`, frontend `vitest` / `tsc --noEmit` / build.
8. Documentation corrections in §7 landed.
9. **A human has connected this client to the deployed server over TLS.**

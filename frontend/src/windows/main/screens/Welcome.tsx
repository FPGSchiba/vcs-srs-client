import { useEffect, useRef, useState } from "react";

import { api } from "../../../shared/api/client";
import { useSession } from "../../../shared/store/session";
import { Field } from "../../../shared/components/Field";
import { Button } from "../../../shared/components/Button";
import { Icon } from "../../../shared/components/Icon";
import { PreLoginTopBar } from "../../../shared/components/PreLoginTopBar";
import { useBuildInfo } from "../../../shared/hooks/useBuildInfo";

type Stage = "welcome" | "manual";

// Mirrors the server's UnitId validation exactly (vngd-srs-server): 2-4
// uppercase letters/digits. The server rejects anything else, including an
// empty string, with no useful explanation surfaced to the user -- this
// catches it client-side instead, before a round trip to the server.
const UNIT_ID_PATTERN = /^[A-Z0-9]{2,4}$/;

function unitIdError(unitId: string): string | null {
  if (!UNIT_ID_PATTERN.test(unitId)) {
    // The field auto-uppercases as the user types (see the FFID input's
    // onChange below), so a rejection here is never a case mismatch --
    // don't mention "A-Z" in a way that reads as "any letters" to someone
    // who typed lowercase. What's left to explain is length/characters.
    return "FFID must be 2–4 letters and digits only (e.g. VG12).";
  }
  return null;
}

/**
 * Extracts a human-readable message from a binding error. Wails serializes a Go
 * error as a JSON envelope ({message, cause, kind}); we surface only `message`.
 */
function errorMessage(e: unknown): string {
  const raw = e instanceof Error ? e.message : typeof e === "string" ? e : "";
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && "message" in parsed) {
      const m = (parsed as { message: unknown }).message;
      if (typeof m === "string") return m;
    }
  } catch {
    /* raw was not JSON — fall through */
  }
  return raw || "connection failed";
}

/**
 * Welcome is the guest login flow for the main window, ported from the design
 * prototype's welcome.jsx. It covers the `welcome` stage (SSO + guest buttons)
 * and the `manual` guest stage (server address, password, player name, optional
 * FFID). The SSO/coalition flow arrives in a later phase, so the SSO button is
 * rendered disabled. Submitting CONNECT drives the session phase and calls
 * api.connect; success transitions arrive via control:connection events wired
 * in MainApp. classNames match the design so the ported CSS applies unchanged.
 *
 * Phase 6.9 CL1: the manual stage is a <form> (Enter submits; BACK is
 * type="button"), the server field is prefilled with the last successfully
 * connected address, the connecting panel has a CANCEL, and the error slot is
 * a role="alert" that also carries the session-ended notice -- when one is
 * set, Welcome opens straight on the manual stage.
 */
export function Welcome() {
  const setPhase = useSession((s) => s.setPhase);
  const phase = useSession((s) => s.phase);
  const build = useBuildInfo();
  const sessionNotice = useSession((s) => s.sessionNotice);
  const setSessionNotice = useSession((s) => s.setSessionNotice);
  const [stage, setStage] = useState<Stage>(() =>
    useSession.getState().sessionNotice ? "manual" : "welcome",
  );
  const [server, setServer] = useState("localhost:5002");
  // serverTouched guards the remembered-address prefill: a late answer never
  // overwrites what the user already typed.
  const serverTouched = useRef(false);
  // cancelledRef marks a connect the user CANCELled, so its rejection is not
  // shown as an error (D3). Reset at the start of every connect().
  const cancelledRef = useRef(false);
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [ffid, setFfid] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    api
      .getLastServerAddress()
      .then((addr) => {
        if (live && addr && !serverTouched.current) setServer(addr);
      })
      .catch(() => {
        /* keep the localhost:5002 fallback */
      });
    return () => {
      live = false;
    };
  }, []);

  async function connect() {
    cancelledRef.current = false;
    setSessionNotice(null);
    setError(null);
    const idError = unitIdError(ffid);
    if (idError) {
      setError(idError);
      return;
    }
    try {
      setPhase("connecting");
      await api.connect(server, name, password, ffid);
      // success transitions to the home screen arrive via the control:connection
      // event wired in MainApp (phase → "connected").
    } catch (e) {
      setPhase("welcome");
      if (!cancelledRef.current) setError(errorMessage(e));
    }
  }

  function cancel() {
    cancelledRef.current = true;
    void api.cancelConnect();
  }

  const alertText = error ?? sessionNotice;

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", background: "var(--bg-0)" }}>
      <PreLoginTopBar />
      <div style={{ flex: 1, position: "relative", display: "grid", placeItems: "center", overflow: "hidden", minHeight: 0 }}>
        {/* faint backdrop */}
        <div
          style={{
            position: "absolute",
            inset: 0,
            background:
              "radial-gradient(ellipse at top, rgba(96,165,250,0.10), transparent 60%)," +
              "radial-gradient(ellipse at bottom, rgba(96,165,250,0.03), transparent 50%)",
            pointerEvents: "none",
          }}
        />
        <div
          style={{
            position: "absolute",
            inset: 0,
            backgroundImage:
              "linear-gradient(rgba(96,165,250,0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(96,165,250,0.04) 1px, transparent 1px)",
            backgroundSize: "40px 40px",
            maskImage: "radial-gradient(ellipse at center, black 30%, transparent 70%)",
            WebkitMaskImage: "radial-gradient(ellipse at center, black 30%, transparent 70%)",
            pointerEvents: "none",
          }}
        />
        {/* radar rings */}
        <div style={{ position: "absolute", top: "50%", left: "50%", width: 720, height: 720, marginLeft: -360, marginTop: -360, borderRadius: "50%", border: "1px solid rgba(96,165,250,0.06)", pointerEvents: "none" }} />
        <div style={{ position: "absolute", top: "50%", left: "50%", width: 520, height: 520, marginLeft: -260, marginTop: -260, borderRadius: "50%", border: "1px solid rgba(96,165,250,0.10)", pointerEvents: "none" }} />

        <div style={{ width: 460, position: "relative", zIndex: 2 }}>
          <div className="col center gap-3" style={{ marginBottom: 28 }}>
            <svg width="64" height="64" viewBox="0 0 64 64" fill="none">
              <circle cx="32" cy="32" r="28" stroke="var(--ac-primary)" strokeWidth="1" opacity="0.4" />
              <circle cx="32" cy="32" r="16" stroke="var(--ac-primary)" strokeWidth="1" />
              <path d="M16 38 L32 18 L48 38" stroke="var(--ac-primary)" strokeWidth="1.5" fill="none" />
              <circle cx="32" cy="32" r="3" fill="var(--ac-primary)" />
              <line x1="32" y1="4" x2="32" y2="12" stroke="var(--ac-primary)" strokeWidth="1" />
              <line x1="32" y1="52" x2="32" y2="60" stroke="var(--ac-primary)" strokeWidth="1" />
              <line x1="4" y1="32" x2="12" y2="32" stroke="var(--ac-primary)" strokeWidth="1" />
              <line x1="52" y1="32" x2="60" y2="32" stroke="var(--ac-primary)" strokeWidth="1" />
            </svg>
            <div className="cap mono" style={{ color: "var(--ac-primary)", fontSize: 11, letterSpacing: "0.32em" }}>VANGUARD COMMUNICATIONS SYSTEM</div>
            <div style={{ fontSize: 28, fontWeight: 600, letterSpacing: "0.08em", color: "var(--tx-0)" }}>V.C.S</div>
            <div className="cap" style={{ color: "var(--tx-3)" }}>FLEET COMMS · TACTICAL · ENCRYPTED</div>
          </div>

          <div className="panel" style={{ background: "rgba(10,20,32,0.7)", backdropFilter: "blur(6px)", borderColor: "var(--bd-3)" }}>
            {phase === "connecting" && (
              <div className="col gap-4" style={{ padding: 28 }}>
                <div className="row acenter gap-3">
                  <Icon name="server" size={14} style={{ color: "var(--ac-primary)" }} />
                  <span className="cap" style={{ color: "var(--ac-primary)" }}>ESTABLISHING TACTICAL LINK</span>
                </div>
                <div className="cap cap-dim">Connecting to {server}…</div>
                <div className="row between acenter">
                  <div className="conn-pill warn" style={{ alignSelf: "flex-start" }}>
                    <span className="dot" /> NEGOTIATING
                  </div>
                  <Button variant="ghost" size="sm" type="button" onClick={cancel}>CANCEL</Button>
                </div>
              </div>
            )}

            {phase !== "connecting" && stage === "welcome" && (
              <div className="col gap-4" style={{ padding: 28 }}>
                <Button variant="primary" size="lg" disabled title="Arrives in a later phase">
                  <Icon name="shield" size={14} /> LOGIN · ORG MEMBER (SSO)
                </Button>
                <Button size="lg" onClick={() => setStage("manual")}>
                  <Icon name="server" size={14} /> JOIN AS GUEST · MANUAL SERVER
                </Button>
              </div>
            )}

            {phase !== "connecting" && stage === "manual" && (
              <form
                className="col gap-5"
                style={{ padding: 28 }}
                noValidate
                onSubmit={(e) => {
                  e.preventDefault();
                  void connect();
                }}
              >
                <div className="row between acenter">
                  <div className="cap" style={{ color: "var(--ac-primary)" }}>MANUAL SERVER · GUEST</div>
                  <Button
                    variant="ghost"
                    size="sm"
                    type="button"
                    onClick={() => {
                      setSessionNotice(null);
                      setStage("welcome");
                    }}
                  >
                    BACK
                  </Button>
                </div>
                <Field label="Server Address" htmlFor="srv">
                  <input
                    id="srv"
                    className="input mono"
                    value={server}
                    onChange={(e) => {
                      serverTouched.current = true;
                      setServer(e.target.value);
                    }}
                  />
                </Field>
                <Field label="Server Password" htmlFor="srv-pw">
                  <input
                    id="srv-pw"
                    className="input"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </Field>
                <div className="row gap-4">
                  <Field label="FFID" htmlFor="ffid" style={{ flex: 1 }}>
                    <input
                      id="ffid"
                      className="input mono"
                      placeholder="e.g. VG12"
                      value={ffid}
                      // Auto-uppercase as typed -- matches how FFIDs are
                      // normally presented and removes the lowercase
                      // rejection failure mode entirely, instead of making
                      // the user decode an ambiguous error message.
                      onChange={(e) => setFfid(e.target.value.toUpperCase())}
                    />
                  </Field>
                  <Field label="Player Name" htmlFor="player" style={{ flex: 1 }}>
                    <input
                      id="player"
                      className="input"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                    />
                  </Field>
                </div>
                {alertText && (
                  <div className="cap" role="alert" style={{ color: "var(--ac-alert)" }}>{alertText}</div>
                )}
                <Button variant="primary" size="lg" type="submit">
                  CONNECT
                </Button>
              </form>
            )}
          </div>

          <div className="row center gap-4" style={{ marginTop: 16, color: "var(--tx-4)", fontFamily: "var(--ff-mono)", fontSize: 9, letterSpacing: "0.16em" }}>
            <span>v{build?.client_version ?? "—"}</span>
            <span>·</span>
            <span>SRS PROTOCOL {build?.protocol_version ?? "—"}</span>
            <span>·</span>
            <span>BUILD {build?.build ?? "—"}</span>
          </div>
        </div>
      </div>
    </div>
  );
}

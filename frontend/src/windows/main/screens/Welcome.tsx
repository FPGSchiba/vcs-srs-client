import { useState } from "react";

import { api } from "../../../shared/api/client";
import { useSession } from "../../../shared/store/session";
import { Field } from "../../../shared/components/Field";
import { Button } from "../../../shared/components/Button";
import { Icon } from "../../../shared/components/Icon";
import { PreLoginTopBar } from "../../../shared/components/PreLoginTopBar";
import { useBuildInfo } from "../../../shared/hooks/useBuildInfo";

type Stage = "welcome" | "manual";

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
 */
export function Welcome() {
  const setPhase = useSession((s) => s.setPhase);
  const build = useBuildInfo();
  const [stage, setStage] = useState<Stage>("welcome");
  const [server, setServer] = useState("localhost:5002");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [ffid, setFfid] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function connect() {
    setError(null);
    try {
      setPhase("connecting");
      await api.connect(server, name, password, ffid);
      // success transitions arrive via control:connection events (wired in MainApp)
    } catch (e) {
      setPhase("welcome");
      setError(errorMessage(e));
    }
  }

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
            {stage === "welcome" && (
              <div className="col gap-4" style={{ padding: 28 }}>
                <Button variant="primary" size="lg" disabled title="Arrives in a later phase">
                  <Icon name="shield" size={14} /> LOGIN · ORG MEMBER (SSO)
                </Button>
                <Button size="lg" onClick={() => setStage("manual")}>
                  <Icon name="server" size={14} /> JOIN AS GUEST · MANUAL SERVER
                </Button>
              </div>
            )}

            {stage === "manual" && (
              <div className="col gap-5" style={{ padding: 28 }}>
                <div className="row between acenter">
                  <div className="cap" style={{ color: "var(--ac-primary)" }}>MANUAL SERVER · GUEST</div>
                  <Button variant="ghost" size="sm" onClick={() => setStage("welcome")}>BACK</Button>
                </div>
                <Field label="Server Address" htmlFor="srv">
                  <input
                    id="srv"
                    className="input mono"
                    value={server}
                    onChange={(e) => setServer(e.target.value)}
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
                  <Field label="FFID (optional)" htmlFor="ffid" style={{ flex: 1 }}>
                    <input
                      id="ffid"
                      className="input mono"
                      value={ffid}
                      onChange={(e) => setFfid(e.target.value)}
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
                {error && (
                  <div className="cap" style={{ color: "var(--ac-alert)" }}>{error}</div>
                )}
                <Button variant="primary" size="lg" onClick={() => void connect()}>
                  CONNECT
                </Button>
              </div>
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

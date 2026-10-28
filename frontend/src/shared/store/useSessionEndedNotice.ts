import { useEffect } from "react";
import { EV, on, type SessionEndedPayload } from "../api/events";
import { useSession } from "./session";

/**
 * sessionEndedMessage maps the server's session-ended reason to the copy the
 * Welcome screen shows (Phase 6.9 CL1, D2 -- user decision).
 */
export function sessionEndedMessage(reason: string): string {
  switch (reason) {
    case "kicked":
      return "You were removed from the server. Log in again.";
    case "banned":
      return "You are banned from this server.";
    default:
      return "Your session has ended. Log in again.";
  }
}

/**
 * useSessionEndedNotice records why the server ended the session, so the
 * Welcome screen MainApp routes to on the following `logged_out` can open on
 * the manual stage with the message in its alert slot. Mounted by MainApp.
 */
export function useSessionEndedNotice(): void {
  const setSessionNotice = useSession((s) => s.setSessionNotice);
  useEffect(
    () =>
      on<SessionEndedPayload>(EV.authSessionEnded, (p) =>
        setSessionNotice(sessionEndedMessage(p?.reason ?? "")),
      ),
    [setSessionNotice],
  );
}

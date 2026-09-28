import { useState } from "react";
import { Button } from "../../../../../../shared/components/Button";
import { api } from "../../../../../../shared/api/client";
import { useSettings } from "../../../../../../shared/store/settings";

/**
 * PermissionCard is the REMEDIATION half of what used to be the Keybinds
 * banner. Its announcement half is now a notification (see Go's
 * App.notifyHotkeyState), which is the right home: a registration failure is
 * exactly the kind of thing the user must learn about WITHOUT having
 * Settings open.
 *
 * What stays here is what the user can only act on here. `requested` and
 * `promptSpent` are local rather than store-backed because neither is
 * observable from the backend: macOS gives no way to ask "has this app's
 * one-shot prompt been used", so the only evidence is a request that came
 * back without prompting. Lifting them into Go would mean inventing backend
 * state that exists solely to track a macOS prompt.
 *
 * Rendered on `permission === "denied"` rather than on `!registered`, which
 * is a deliberate change from the banner it replaces: keyed on the old
 * condition it would vanish the moment registration happened to succeed
 * while the grant was still absent. Only macOS ever reports "denied";
 * "not_applicable" (Windows, Linux/X11) and "unknown" render nothing,
 * because there is nothing to grant and a button would be a dead end.
 */
export function PermissionCard() {
  const hotkeys = useSettings((s) => s.hotkeys);

  // `requested` unlocks RE-CHECK; `promptSpent` swaps GRANT ACCESS for OPEN
  // SETTINGS. Both are local rather than derived from the store because
  // neither is observable from the backend: macOS gives no way to ask "has
  // this app's one-shot prompt been used", so the only evidence is a request
  // that came back without prompting.
  const [requested, setRequested] = useState(false);
  const [promptSpent, setPromptSpent] = useState(false);

  const handleGrantPermission = async () => {
    setRequested(true);
    try {
      const res = await api.requestHotkeyPermission();
      // `prompted` is NOT the user's answer (the OS resolves the prompt
      // asynchronously and never reports the decision back through this
      // call). It is only evidence about whether a prompt could still be
      // shown: no prompt AND still not granted means the one-shot is spent,
      // and System Settings is the only remaining route.
      setPromptSpent(!res.prompted && res.permission !== "granted");
    } catch (err) {
      // Logged, not surfaced: the notification already says hotkeys are
      // unavailable, and RE-CHECK stays available as the retry.
      console.error("requestHotkeyPermission failed", err);
    }
  };

  const handleOpenPermissionSettings = () => {
    void api.openHotkeyPermissionSettings().catch((err) => {
      console.error("openHotkeyPermissionSettings failed", err);
    });
  };

  const handleRecheckPermission = () => {
    void api.recheckHotkeyPermission().catch((err) => {
      console.error("recheckHotkeyPermission failed", err);
    });
  };

  // Only macOS ever reports "denied". "not_applicable" (Windows, Linux/X11)
  // and "unknown" must render no permission affordance at all -- there is
  // nothing for the user to grant, so a button would be a dead end.
  const permissionDenied = hotkeys.permission === "denied";
  // Access is in place and hotkeys STILL will not register. The backend
  // already re-applied on the grant (which recreates the event tap and
  // usually suffices), so if this shows, a restart is the remaining step.
  // Text only, deliberately: a programmatic relaunch is platform-specific,
  // easy to get wrong, and mostly unnecessary.
  const grantedButUnregistered = hotkeys.permission === "granted" && !hotkeys.registered;

  if (!permissionDenied && !grantedButUnregistered) return null;

  return (
    <div className="col gap-3" style={{ padding: "8px 12px" }}>
      {permissionDenied && (
        <>
          <span
            className="cap-dim"
            style={{ fontSize: 10, textTransform: "none", letterSpacing: "0.04em" }}
          >
            macOS requires Accessibility permission for global hotkeys (System
            Settings → Privacy &amp; Security → Accessibility). Until it is granted,
            the system delivers no keypress to VCS while another application is
            focused.
          </span>
          <div className="row gap-3">
            {promptSpent ? (
              <Button size="sm" onClick={handleOpenPermissionSettings}>
                OPEN SETTINGS
              </Button>
            ) : (
              <Button size="sm" variant="primary" onClick={handleGrantPermission}>
                GRANT ACCESS
              </Button>
            )}
            {requested && (
              <Button size="sm" variant="ghost" onClick={handleRecheckPermission}>
                RE-CHECK
              </Button>
            )}
          </div>
        </>
      )}

      {grantedButUnregistered && (
        <span
          className="cap-dim"
          style={{ fontSize: 10, textTransform: "none", letterSpacing: "0.04em" }}
        >
          Accessibility is granted — restart VCS for hotkeys to take effect.
        </span>
      )}
    </div>
  );
}

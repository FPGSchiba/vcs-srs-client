import { useMemo, useState } from "react";
import { api } from "../../shared/api/client";
import { Icon } from "../../shared/components/Icon";
import { categoryFor } from "../../shared/components/notificationCategories";
import { useNotifications, type NotifyAction } from "../../shared/store/notifications";
import { useNotificationsSync } from "../../shared/store/useNotificationsSync";
import { NotifRow } from "./NotifRow";

/**
 * NotificationsApp is the Notifications pop-out shell, ported from the
 * design prototype's `screens/misc.jsx:292-343`.
 *
 * The store is a MIRROR: mark-read, mark-all-read and clear-all all
 * round-trip through Go and come back on `notifications:changed`. Nothing is
 * applied optimistically, for the same reason CommsApp never mutates the
 * radios store directly -- the backend is the single source of truth.
 *
 * Close routes through `api.closeWindow("notifications")` rather than the
 * in-webview `Window.Close()`, which is what persists geometry through the
 * registry.
 */
export function NotificationsApp() {
  const snap = useNotifications((s) => s.snap);
  const [filter, setFilter] = useState("all");
  const [expanded, setExpanded] = useState<string | null>(null);

  useNotificationsSync();

  // Only categories actually PRESENT. The shared table has seven; offering
  // all of them here would be a dropdown of six empty options.
  const present = useMemo(() => {
    const keys = new Set(snap.items.map((i) => i.category));
    return Array.from(keys).sort();
  }, [snap.items]);

  const filtered = useMemo(
    () => (filter === "all" ? snap.items : snap.items.filter((i) => i.category === filter)),
    [snap.items, filter],
  );

  const handleToggle = (id: string, unread: boolean) => {
    setExpanded((cur) => (cur === id ? null : id));
    // Opening a row is the user reading it. Routed through the backend so
    // every window's badge agrees.
    if (unread) void api.markNotificationRead(id);
  };

  const handleAction = (a: NotifyAction) => {
    // A closed set of kinds, so a notification source added in a later
    // phase needs no new dispatch code here.
    if (a.kind === "open-window") {
      void api.openWindow(a.target);
      return;
    }
    if (a.kind === "navigate") {
      // The main window owns navigation, and it is NOT a Registry entry --
      // it is a Wails window resolved by name, so openWindow("main") would
      // spawn a second one. Focusing it is the honest thing a popout can
      // do; routing the nav key across windows is Phase 7.4's problem.
      void api.focusMainWindow();
    }
  };

  return (
    <div
      className="popout"
      style={{
        position: "static",
        width: "100%",
        height: "100%",
        border: "none",
        borderRadius: 0,
        boxShadow: "none",
        minWidth: 0,
        minHeight: 0,
      }}
    >
      <div className="popout-chrome">
        <Icon name="bell" size={14} />
        <span className="ttl">Notifications</span>
        <div className="ctrl">
          <button
            type="button"
            className="close"
            aria-label="close"
            title="Close"
            onClick={() => void api.closeWindow("notifications")}
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      </div>

      <div
        style={{
          padding: "10px 14px",
          background: "var(--bg-0)",
          borderBottom: "1px solid var(--bd-1)",
          display: "flex",
          alignItems: "center",
          gap: 10,
        }}
      >
        <span className="cap-dim mono">
          {filtered.length} OF {snap.items.length}
        </span>
        <span className="flex" style={{ flex: 1 }} />
        <select
          className="input"
          aria-label="Filter by category"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ width: 150, height: 24 }}
        >
          <option value="all">All categories</option>
          {present.map((k) => (
            <option key={k} value={k}>
              {categoryFor(k).label}
            </option>
          ))}
        </select>
        <button type="button" className="btn btn-sm" onClick={() => void api.markAllNotificationsRead()}>
          MARK ALL READ
        </button>
        <button type="button" className="btn btn-sm btn-danger" onClick={() => void api.clearNotifications()}>
          CLEAR ALL
        </button>
      </div>

      <div className="popout-body" style={{ overflow: "auto", padding: 10 }}>
        {filtered.length === 0 ? (
          <div className="state-card" style={{ marginTop: 32 }}>
            <div className="state-glyph">
              <Icon name="bell" size={20} />
            </div>
            <div className="state-title">ALL CLEAR</div>
            <div style={{ fontSize: 12, color: "var(--tx-3)", maxWidth: 320, textWrap: "pretty" }}>
              {snap.items.length === 0
                ? "No notifications. New events will appear here as they happen."
                : "No notifications match the current filter."}
            </div>
          </div>
        ) : (
          <div className="col gap-2">
            {filtered.map((it) => (
              <NotifRow
                key={it.id}
                item={it}
                expanded={expanded === it.id}
                onToggle={() => handleToggle(it.id, it.unread)}
                onAction={handleAction}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

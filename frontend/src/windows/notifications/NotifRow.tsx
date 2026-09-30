import { Icon } from "../../shared/components/Icon";
import { activatable } from "../../shared/components/activatable";
import { categoryFor } from "../../shared/components/notificationCategories";
import type { NotifyItem, NotifyAction } from "../../shared/store/notifications";

interface NotifRowProps {
  item: NotifyItem;
  expanded: boolean;
  onToggle: () => void;
  onAction: (action: NotifyAction) => void;
}

/** Renders the item's timestamp as HH:MM:SS, matching the design
 *  prototype's `time` field. Falls back to the raw string rather than
 *  rendering "Invalid Date" if the backend ever sends something unparseable. */
function clock(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString(undefined, { hour12: false });
}

/**
 * NotifRow is one notification, ported from the design prototype's
 * `screens/misc.jsx:346-402`.
 *
 * It renders the GENERIC item and branches on nothing about what produced
 * it. This directory must stay free of any reference to a specific
 * notification source -- that is what lets Phase 7.3 and 7.4 add sources
 * without touching the UI.
 *
 * The header is a clickable non-button, as the prototype's CSS requires, so
 * it carries `role`/`tabIndex`/Enter/Space through the shared `activatable`
 * helper (SonarCloud `typescript:S1082`).
 */
export function NotifRow({ item, expanded, onToggle, onAction }: NotifRowProps) {
  const cat = categoryFor(item.category);
  return (
    <div
      data-key={item.key || undefined}
      data-resolved={item.resolved ? "" : undefined}
      style={{
        border: "1px solid var(--bd-2)",
        borderLeft: `2px solid ${cat.color}`,
        borderRadius: 3,
        background: item.unread ? "var(--bg-2)" : "var(--bg-1)",
        overflow: "hidden",
        opacity: item.resolved ? 0.55 : 1,
      }}
    >
      <div
        {...activatable(onToggle)}
        aria-expanded={expanded}
        style={{
          padding: "10px 12px",
          cursor: "pointer",
          display: "grid",
          gridTemplateColumns: "auto 1fr auto auto",
          gap: 10,
          alignItems: "center",
        }}
      >
        <div style={{ display: "grid", placeItems: "center", width: 24, height: 24, color: cat.color }}>
          <Icon name={item.icon} size={14} />
        </div>
        <div className="col" style={{ minWidth: 0 }}>
          <div className="row acenter gap-3">
            <span className="cap" style={{ color: cat.color }}>
              {cat.label.toUpperCase()}
            </span>
            {item.unread && (
              <span
                data-unread=""
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  background: "var(--ac-primary)",
                  boxShadow: "0 0 4px var(--ac-primary)",
                }}
              />
            )}
            {item.resolved && (
              <span className="cap-dim" style={{ fontSize: 9 }}>
                RESOLVED
              </span>
            )}
          </div>
          <div
            style={{
              fontSize: 12,
              color: "var(--tx-0)",
              marginTop: 2,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: expanded ? "normal" : "nowrap",
              overflowWrap: "anywhere",
            }}
          >
            {item.title}
          </div>
        </div>
        <span className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>
          {clock(item.time)}
        </span>
        <Icon name={expanded ? "chevronU" : "chevronD"} size={11} style={{ color: "var(--tx-3)" }} />
      </div>

      {expanded && (
        <div style={{ padding: "0 12px 12px 46px", color: "var(--tx-2)", fontSize: 12, lineHeight: 1.6 }}>
          {item.body && (
            // overflowWrap is not cosmetic: body carries OS error strings
            // and device names verbatim, and a 400-character malgo error
            // with no spaces would otherwise stretch the row.
            <div data-body="" style={{ textWrap: "pretty", overflowWrap: "anywhere" }}>
              {item.body}
            </div>
          )}
          {item.context.length > 0 && (
            <div
              className="mono"
              style={{
                marginTop: 8,
                padding: 8,
                background: "var(--bg-1)",
                border: "1px solid var(--bd-1)",
                borderRadius: 3,
                fontSize: 11,
                color: "var(--tx-2)",
              }}
            >
              {item.context.map((kv) => (
                <div key={kv.key} className="row gap-3" style={{ padding: "1px 0" }}>
                  <span style={{ color: "var(--tx-3)", width: 80, flexShrink: 0 }}>
                    {kv.key.toUpperCase()}
                  </span>
                  <span style={{ color: "var(--tx-0)", overflowWrap: "anywhere" }}>{kv.value}</span>
                </div>
              ))}
            </div>
          )}
          {item.actions.length > 0 && (
            <div className="row gap-2" style={{ marginTop: 10 }}>
              {item.actions.map((a) => (
                <button
                  key={a.label}
                  type="button"
                  className={`btn btn-sm ${a.primary ? "btn-primary" : ""}`}
                  onClick={() => onAction(a)}
                >
                  <Icon name={a.icon || "chevron"} size={11} /> {a.label}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

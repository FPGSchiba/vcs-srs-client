import type { RadioShellProps } from "./shells";

/**
 * The row shell: a content column with the PTT beside it, vertically centred.
 * The square PTT sets the card's minimum height at HORIZONTAL; at NARROW-H the
 * LCD anchors the left, two text lines take the middle and the PTT anchors the
 * right (design/vcs/radio-variants.md §2).
 */
export function RadioRow({ variant, rid, name, lcd, talker, ptt, chips }: RadioShellProps) {
  const narrow = variant.shows.chips === "none";

  if (narrow) {
    return (
      <div
        data-testid="shell-outer"
        className="row acenter"
        style={{
          display: "flex",
          flexDirection: "row",
          alignItems: "center",
          gap: 10,
          padding: 9,
          height: "100%",
          boxSizing: "border-box",
          minWidth: 0,
        }}
      >
        {lcd}
        <div style={{ display: "flex", flexDirection: "column", gap: 3, flex: 1, minWidth: 0 }}>
          <div
            data-testid="shell-header"
            className="row acenter gap-2"
            style={{ minWidth: 0, justifyContent: "flex-start" }}
          >
            {rid}
            <span data-testid="shell-name-slot" style={{ minWidth: 0, overflow: "hidden", flex: 1 }}>
              {name}
            </span>
          </div>
          <div data-testid="shell-status" style={{ minWidth: 0 }}>
            {talker}
          </div>
        </div>
        {ptt}
      </div>
    );
  }

  return (
    <div
      data-testid="shell-outer"
      style={{
        display: "flex",
        flexDirection: "row",
        alignItems: "center",
        gap: 12,
        padding: "14px 12px 12px",
        height: "100%",
        boxSizing: "border-box",
        minWidth: 0,
      }}
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 8, flex: 1, minWidth: 0 }}>
        <div
          data-testid="shell-header"
          className="row acenter gap-3"
          style={{ minWidth: 0, justifyContent: "flex-start" }}
        >
          {rid}
          <span data-testid="shell-name-slot" style={{ minWidth: 0, overflow: "hidden", flex: 1 }}>
            {name}
          </span>
          {chips}
        </div>
        {/* Left-aligned, not centred: the column's left edge is the card's
            reading line. */}
        <div style={{ display: "flex", alignSelf: "flex-start", minWidth: 0 }}>{lcd}</div>
        <div data-testid="shell-status" style={{ minWidth: 0 }}>
          {talker}
        </div>
      </div>
      {ptt}
    </div>
  );
}

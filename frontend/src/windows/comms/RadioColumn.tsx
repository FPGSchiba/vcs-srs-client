import type { RadioShellProps } from "./shells";

/** Below this LCD size a column is the narrow variant: tighter, no divider. */
const NARROW_LCD_PX = 24;

/**
 * The column shell: header, LCD, then status, stacked.
 *
 * Its height is the SUM of its rows at one uniform gap, with nothing pinned by
 * `margin-top: auto`. That rule is why the vertical card is 166px and not the
 * 260px an earlier draft came to — the difference was not spacing, it was
 * leftover space collecting in a single gap above a pinned status row. See
 * design/vcs/radio-variants.md §3.
 */
export function RadioColumn({ variant, rid, name, lcd, talker, ptt, chips }: RadioShellProps) {
  const narrow = variant.lcdPx < NARROW_LCD_PX;
  const gap = narrow ? 7 : 9;
  const pad = narrow ? "10px" : "14px 12px 12px";

  return (
    <div
      data-testid="shell-outer"
      style={{
        display: "flex",
        flexDirection: "column",
        gap,
        padding: pad,
        height: "100%",
        boxSizing: "border-box",
        minWidth: 0,
      }}
    >
      <div
        data-testid="shell-header"
        className="row acenter gap-3"
        style={{ justifyContent: "flex-start", minWidth: 0, flexShrink: 0 }}
      >
        {rid}
        <span data-testid="shell-name-slot" style={{ minWidth: 0, overflow: "hidden", flex: 1 }}>
          {name}
        </span>
        {variant.shows.chips !== "none" && chips}
      </div>

      <div style={{ display: "flex", justifyContent: "center", minWidth: 0 }}>{lcd}</div>

      {narrow ? (
        <>
          <div data-testid="shell-status" style={{ minWidth: 0 }}>
            {talker}
          </div>
          <div style={{ display: "flex" }}>{ptt}</div>
        </>
      ) : (
        <>
          <div className="row acenter" style={{ justifyContent: "flex-end", minWidth: 0 }}>
            {ptt}
          </div>
          <div
            data-testid="shell-status"
            style={{
              minWidth: 0,
              paddingTop: 6,
              borderTop: variant.shows.statusDivider ? "1px solid var(--bd-1)" : undefined,
            }}
          >
            {talker}
          </div>
        </>
      )}
    </div>
  );
}

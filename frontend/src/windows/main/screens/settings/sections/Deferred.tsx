import { Panel } from "../../../../../shared/components/Panel";

interface DeferredProps {
  title?: string;
  phase?: number;
  items?: string[];
  note?: string;
}

/**
 * Deferred is the honest stub rendered for a Settings section whose backing
 * subsystem does not exist yet, rather than greyed-out replicas of controls
 * that don't do anything. It has two modes:
 *
 * - with a `phase`: the title reads "Arrives in Phase N", for a section a
 *   roadmap phase owns;
 * - without one: the title reads "Not yet scheduled", for a section no phase
 *   owns yet.
 *
 * `items` lists what the section will hold. `note`, when set, adds one dim
 * line under the title, e.g. to point at where a related setting already
 * lives.
 */
export function Deferred({ title = "COMING SOON", phase, items = [], note }: DeferredProps) {
  return (
    <Panel title={title}>
      <div className="state-card">
        <div className="state-title">
          {phase !== undefined ? `Arrives in Phase ${phase}` : "Not yet scheduled"}
        </div>
        {note && (
          <div className="cap-dim" style={{ fontSize: 11 }}>
            {note}
          </div>
        )}
        {items.length > 0 && (
          <ul style={{ listStyle: "none", padding: 0, margin: 0 }} className="col gap-2">
            {items.map((item) => (
              <li key={item} className="cap-dim" style={{ fontSize: 11 }}>
                {item}
              </li>
            ))}
          </ul>
        )}
      </div>
    </Panel>
  );
}

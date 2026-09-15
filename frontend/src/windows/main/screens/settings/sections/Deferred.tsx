import { Panel } from "../../../../../shared/components/Panel";

interface DeferredProps {
  title?: string;
  phase: number;
  items?: string[];
}

/**
 * Deferred is the honest stub rendered for a Settings section whose backing
 * subsystem does not exist yet. It states plainly which phase brings the real
 * controls and lists what's coming, rather than rendering greyed-out replicas
 * of controls that don't do anything. One parameterised component covers all
 * six not-yet-built sections (see the section/phase table in the Task 10
 * brief) plus the temporary Keybinds placeholder pending Task 11.
 */
export function Deferred({ title = "COMING SOON", phase, items = [] }: DeferredProps) {
  return (
    <Panel title={title}>
      <div className="state-card">
        <div className="state-title">Arrives in Phase {phase}</div>
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

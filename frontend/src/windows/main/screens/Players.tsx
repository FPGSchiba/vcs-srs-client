import { useClients } from "../../../shared/store/clients";

/**
 * Players renders the live connected-client roster as a `.tbl` table, one row
 * per entry in the clients store. The store is fed by `state:client_update` /
 * `state:client_left` events wired in MainApp, so the table updates live as
 * clients join and leave. Columns mirror the ClientInfoDTO shape (name,
 * coalition, unit_id, role_id). classNames match the ported design CSS.
 */
export function Players() {
  const clients = useClients((s) => s.clients);
  const entries = Object.entries(clients);

  return (
    <div style={{ padding: 14, height: "100%", overflow: "auto" }}>
      <table className="tbl">
        <thead>
          <tr>
            <th>Name</th>
            <th>Coalition</th>
            <th>Unit</th>
            <th>Role</th>
          </tr>
        </thead>
        <tbody>
          {entries.length === 0 ? (
            <tr>
              <td colSpan={4} style={{ color: "var(--tx-3)" }}>
                No connected clients
              </td>
            </tr>
          ) : (
            entries.map(([guid, c]) => (
              <tr key={guid}>
                <td>{c.name}</td>
                <td>{c.coalition}</td>
                <td>{c.unit_id}</td>
                <td>{c.role_id}</td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

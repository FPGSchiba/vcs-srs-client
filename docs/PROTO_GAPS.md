# Proto extension candidates

Features that exist in the VCS client design but are **not** in `srs.proto` today. The client implements them locally for v1 with clear extension hooks; the server team may want to fold them into the wire later.

> Operations is **not** in this list — it lives on a REST endpoint owned by another system.

---

## 1. Per-radio encryption and channel

**Where it shows up:** Comms popout per-radio strip (`.kbd` enc toggle + channel `Ch.` selector).

**Today:** Local-only on the client. Not transmitted to other clients.

**Suggested proto change:**

```proto
message Radio {
  uint32 id          = 1;
  string name        = 2;
  float  frequency   = 3;
  bool   enabled     = 6;
  bool   is_intercom = 7;
  bool   encrypted   = 8;   // NEW
  uint32 enc_channel = 9;   // NEW
}
```

Server-side, voice payloads on `encrypted=true` radios use a per-(coalition, channel) key derived from the existing `VoiceHostDetails.secret`.

---

## 2. Per-client presence status

**Where it shows up:** User menu + nav-rail user chip + Player List row (`pill-available`, `pill-combat`, `pill-discipline`, `pill-afk`).

**Today:** Local-only on the client. Visible to self in the UI; not propagated.

**Suggested proto change:**

```proto
enum Presence {
  PRESENCE_UNKNOWN     = 0;
  PRESENCE_AVAILABLE   = 1;
  PRESENCE_COMBAT      = 2;
  PRESENCE_DISCIPLINE  = 3;
  PRESENCE_AFK         = 4;
}

message ClientInfo {
  string   name        = 1;
  string   coalition   = 2;
  string   unit_id     = 3;
  uint32   role_id     = 4;
  optional int64 last_update = 5;
  Presence presence    = 6;   // NEW
}
```

`UpdateClientInfo` already carries `ClientInfo`, so the wire path is free; only the field is new.

---

## 3. Ship-mode component registry

**Where it shows up:** Ship Mode popout (Persephone components, health bars, shield arc, etc.).

**Today:** Local TOML + JSON. Components defined in `ships.toml`.

**Suggested proto change:** Future `ShipModeService` with a server-streamed registry of components per ship type, so multiple crew members see the same source of truth. Out of scope for client v1, but the client's local data model is shaped to absorb a server stream later without UI changes.

---

## 4. Ships catalog

**Where it shows up:** Ship picker in Ship Mode popout.

**Today:** TOML file `ships.toml`.

**Suggested proto change:** Future `GetShipCatalog` RPC returning canonical ship metadata (name, role, max crew, default components).

---

## 5. Roles catalog

**Where it shows up:** Role picker after `LoginResult`; player list role badges.

**Today:** Partial — `RoleSelection` exists in auth flow. Client locally mirrors it in `roles.toml` for offline rendering of player-list role names.

**Suggested proto change:** Make `RoleSelection` (or a new `Role` message with `id`, `name`, `permissions`) reusable from `SRSService` so any client can resolve `ClientInfo.role_id` → human-readable role without auth state.

---

## 6. Text channels per frequency

**Where it shows up:** Messages popout (one tab per frequency, mirroring radios).

**Today:** Local in-memory ring buffer; messages never leave the client.

**Suggested proto change:** Future `MessagingService` with `SendMessage(freq, body)` and `SubscribeMessages(freq)` stream. Mirrors the SRS frequency-as-channel model.

---

## 7. Transmission history

**Where it shows up:** Transmission Log nav screen + status bar "history" affordance.

**Today:** Local — populated client-side from observed `voice:rx_active` events; written to a JSON ring buffer file.

**Suggested proto change:** Future server-pushed history so clients see a global, durable log (useful for after-action review).

---

## 8. Notifications

**Where it shows up:** Notifications popout + alert bell in status bar.

**Today:** Local notifications only (toast stack already exists in the design's `app.jsx`).

**Suggested proto change:** Future `NotificationsService` with a server-pushed alert stream (`Notification { id, kind, title, body, source_client_guid?, expiry }`), e.g. for distress beacons, server-wide announcements, admin messages.

---

## 9. Recording

**Where it shows up:** Per-radio record button.

**Today:** Local Opus files written by the client only.

**Suggested proto change:** None — recording stays client-side by design.

---

## How to use this document

When the server team wants to extend `srs.proto`, this list is the priority queue. Each item also pairs with a client commit that swaps the local fallback for the wire-backed implementation; those commits should bump the `ClientCapabilities.version` so the server can degrade gracefully for older clients.

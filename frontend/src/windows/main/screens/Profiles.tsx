import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../../../shared/api/client";
import { place } from "../../../shared/layout";
import type {
  LayoutBlock,
  LayoutWindow,
  ProfileState,
  ProfileSummary,
} from "../../../shared/api/client";
import { EV, on } from "../../../shared/api/events";
import { useProfile } from "../../../shared/store/profile";
import { Icon } from "../../../shared/components/Icon";
import { Field } from "../../../shared/components/Field";

const VIEW_W = 42;
const VIEW_H = 28;
const MARGIN = 1;

/**
 * LayoutPreview draws a schematic of the STORED block sizes, scaled
 * uniformly into the prototype's 42x28 viewBox. It renders exactly one
 * <rect> per block and deliberately no frame rect -- the count is the
 * contract. It replaces the prototype's three hardcoded schematics
 * (2x2 / POWER / STRIP), which were presets this format does not have.
 */
function LayoutPreview({ blocks, window: win }: Readonly<{ blocks: LayoutBlock[]; window: LayoutWindow }>) {
  const { rects, w, h } = place(blocks, win);
  const scale = Math.min((VIEW_W - 2 * MARGIN) / w, (VIEW_H - 2 * MARGIN) / Math.max(h, 1));
  return (
    <svg data-testid="layout-preview" width={VIEW_W} height={VIEW_H} viewBox={`0 0 ${VIEW_W} ${VIEW_H}`} aria-hidden="true">
      {rects.map((r, i) => (
        <rect
          key={blocks[i].radio_id}
          x={MARGIN + r.x * scale}
          y={MARGIN + r.y * scale}
          width={Math.max(r.w * scale, 1)}
          height={Math.max(r.h * scale, 1)}
          fill="none"
          stroke="var(--ac-primary)"
          opacity="0.6"
        />
      ))}
    </svg>
  );
}

// A second DELETE click within this window of arming is ignored, so a stray
// double-click cannot arm-and-fire; the armed state also times out.
const DELETE_COOLDOWN_MS = 350;
const DELETE_ARM_TIMEOUT_MS = 3000;

const fileName = (path: string) => path.split(/[\\/]/).pop() ?? path;

/**
 * Profiles is the Radio Profiles nav screen, ported from the design
 * prototype's `ScreenProfiles`.
 *
 * Deliberate deviations from the prototype, all forced by what exists:
 *  - the "EXAMPLE CONTENT" <pre> is DROPPED: it showed fabricated JSON with
 *    enc/key fields this format does not have, and fake content beside a real
 *    file is worse than none;
 *  - the duplicate (copy) button is DROPPED: there is no DuplicateProfile
 *    binding, and a control that does nothing is the bug shape to avoid;
 *  - SAVE CURRENT AS NEW, RENAME and DELETE confirmation are inline controls,
 *    never window.prompt/confirm, which are blocking modal dialogs.
 *
 * Profile state is Go-owned; this hydrates on mount and follows
 * `profile:state`.
 */
export function Profiles() {
  const state = useProfile((s) => s.state);
  const profiles = useProfile((s) => s.profiles);
  const [selected, setSelected] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [saving, setSaving] = useState(false);
  const [newName, setNewName] = useState("");
  const [renaming, setRenaming] = useState<{ path: string; name: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [error, setError] = useState("");
  const saveRef = useRef<HTMLInputElement>(null);
  const renameRef = useRef<HTMLInputElement>(null);
  const armedAt = useRef(0);
  const armTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => () => clearTimeout(armTimer.current), []);
  // Focus the inline editors as they open (both are user-initiated).
  useEffect(() => {
    if (saving) saveRef.current?.focus();
  }, [saving]);
  const renamingPath = renaming?.path;
  useEffect(() => {
    if (renamingPath) renameRef.current?.focus();
  }, [renamingPath]);

  const disarm = (path?: string) => {
    clearTimeout(armTimer.current);
    setConfirmDelete((c) => (path === undefined || c === path ? null : c));
  };

  const onDelete = (path: string) => {
    if (confirmDelete !== path) {
      armedAt.current = Date.now();
      clearTimeout(armTimer.current);
      armTimer.current = setTimeout(() => setConfirmDelete(null), DELETE_ARM_TIMEOUT_MS);
      setConfirmDelete(path);
      return;
    }
    if (Date.now() - armedAt.current < DELETE_COOLDOWN_MS) return;
    disarm();
    void run(() => api.deleteProfile(path));
  };

  useEffect(() => {
    const refresh = () =>
      useProfile
        .getState()
        .refresh()
        .catch(() => {
          /* not wired yet -- ignore */
        });
    void refresh();
    const off = on<ProfileState>(EV.profileState, (s) => {
      useProfile.getState().setState(s);
      void refresh();
    });
    return () => off();
  }, []);

  const run = async (fn: () => Promise<void>) => {
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const rows = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? profiles.filter((p) => p.name.toLowerCase().includes(q)) : profiles;
  }, [profiles, filter]);

  const detail = profiles.find((p) => p.path === selected);

  const submitSave = () => {
    const name = newName.trim();
    if (!name) return;
    void run(async () => {
      await api.saveProfileAs(name, "");
      setSaving(false);
      setNewName("");
    });
  };

  const submitRename = (p: ProfileSummary) => {
    const name = renaming?.name.trim();
    if (!name) return;
    void run(async () => {
      await api.renameProfile(p.path, name, p.description);
      setRenaming(null);
    });
  };

  return (
    <div style={{ height: "100%", display: "grid", gridTemplateColumns: "1fr 360px", minHeight: 0 }}>
      <div style={{ overflow: "auto", padding: 16, minHeight: 0 }}>
        <div className="panel">
          <div className="panel-h">
            <span className="cap">◆ PROFILES DIRECTORY</span>
            <div className="row gap-3">
              <input
                className="input mono"
                aria-label="Profiles directory"
                readOnly
                value={state.dir}
                style={{ width: 380 }}
              />
              <button type="button" className="btn btn-sm" onClick={() => void run(api.browseProfilesDir)}>
                <Icon name="folder" size={11} /> BROWSE
              </button>
              <button type="button" className="btn btn-sm" onClick={() => void run(api.openProfilesDir)}>
                <Icon name="folder" size={11} /> OPEN
              </button>
            </div>
          </div>
          <div className="panel-body">
            <div className="row acenter gap-4" style={{ marginBottom: 12 }}>
              <input
                className="input flex"
                placeholder="Filter profiles by name…"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
              <button type="button" className="btn" onClick={() => void run(api.importProfile)}>
                <Icon name="upload" size={11} /> IMPORT FROM FILE
              </button>
              {saving ? (
                <>
                  <input
                    className="input"
                    aria-label="New profile name"
                    placeholder="New profile name"
                    ref={saveRef}
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") submitSave();
                      if (e.key === "Escape") setSaving(false);
                    }}
                  />
                  <button
                    type="button"
                    className="btn btn-primary"
                    disabled={!newName.trim()}
                    onClick={submitSave}
                  >
                    <Icon name="save" size={11} /> SAVE
                  </button>
                  <button type="button" className="btn" onClick={() => setSaving(false)}>
                    CANCEL
                  </button>
                </>
              ) : (
                <>
                  {state.dirty && state.active_path && (
                    <button
                      type="button"
                      className="btn btn-primary"
                      title="Save changes into the active profile"
                      onClick={() => void run(api.saveProfile)}
                    >
                      <Icon name="save" size={11} /> SAVE
                    </button>
                  )}
                  <button type="button" className="btn btn-primary" onClick={() => setSaving(true)}>
                    <Icon name="save" size={11} /> SAVE CURRENT AS NEW
                  </button>
                </>
              )}
            </div>
            {error && (
              <div role="alert" className="cap" style={{ color: "var(--ac-alert)", marginBottom: 8 }}>
                {error}
              </div>
            )}
            {rows.length === 0 ? (
              <div style={{ color: "var(--tx-3)", padding: 24, textAlign: "center" }}>
                No profiles yet
              </div>
            ) : (
              <table className="tbl">
                <thead>
                  <tr>
                    <th style={{ width: 60 }}>Preview</th>
                    <th>Name</th>
                    <th>Filename</th>
                    <th>Modified</th>
                    <th>Radios</th>
                    <th style={{ width: 180 }}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((p) => (
                    <tr
                      key={p.path}
                      style={{ background: selected === p.path ? "rgba(96,165,250,0.04)" : undefined }}
                      // Mouse convenience only: keyboard and assistive tech select
                      // through the name button below (a <tr> cannot be a button).
                      onClick={() => setSelected(p.path)}
                    >
                      <td>
                        <LayoutPreview blocks={p.blocks ?? []} window={p.window} />
                      </td>
                      <td>
                        {renaming?.path === p.path ? (
                          <input
                            className="input"
                            aria-label="Profile name"
                            ref={renameRef}
                            value={renaming.name}
                            onClick={(e) => e.stopPropagation()}
                            onChange={(e) => setRenaming({ path: p.path, name: e.target.value })}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") submitRename(p);
                              if (e.key === "Escape") setRenaming(null);
                            }}
                          />
                        ) : (
                          <button
                            type="button"
                            aria-pressed={selected === p.path}
                            style={{ color: "var(--tx-0)", fontWeight: 500, textAlign: "left" }}
                          >
                            {p.name}
                          </button>
                        )}
                        {p.path === state.active_path && (
                          <span className="cap" style={{ color: "var(--ac-primary)", marginLeft: 8 }}>
                            ● ACTIVE
                          </span>
                        )}
                      </td>
                      <td className="mono" style={{ fontSize: 10, color: "var(--tx-3)" }}>
                        {fileName(p.path)}
                      </td>
                      <td className="mono" style={{ fontSize: 10 }}>
                        {p.modified}
                      </td>
                      <td className="mono">{p.radio_count}</td>
                      <td>
                        <div className="row gap-2">
                          <button
                            type="button"
                            className="btn btn-sm"
                            onClick={(e) => {
                              e.stopPropagation();
                              void run(() => api.loadProfile(p.path));
                            }}
                          >
                            LOAD
                          </button>
                          <button
                            type="button"
                            className="btn btn-sm"
                            aria-label={`Rename ${p.name}`}
                            onClick={(e) => {
                              e.stopPropagation();
                              setRenaming({ path: p.path, name: p.name });
                            }}
                          >
                            <Icon name="edit" size={10} />
                          </button>
                          <button
                            type="button"
                            className="btn btn-sm"
                            aria-label={`Export ${p.name}`}
                            onClick={(e) => {
                              e.stopPropagation();
                              void run(() => api.exportProfile(p.path));
                            }}
                          >
                            <Icon name="download" size={10} />
                          </button>
                          <button
                            type="button"
                            className="btn btn-sm btn-danger"
                            aria-label={
                              confirmDelete === p.path ? `Confirm delete ${p.name}` : `Delete ${p.name}`
                            }
                            onClick={(e) => {
                              e.stopPropagation();
                              onDelete(p.path);
                            }}
                            onBlur={() => disarm(p.path)}
                          >
                            {confirmDelete === p.path ? "CONFIRM?" : <Icon name="trash" size={10} />}
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </div>

      <div style={{ borderLeft: "1px solid var(--bd-1)", background: "var(--bg-0)", padding: 16, overflow: "auto" }}>
        <span className="cap" style={{ color: "var(--ac-primary)" }}>
          PROFILE DETAILS
        </span>
        {detail && (
          <div className="col gap-4" style={{ marginTop: 10 }}>
            <Field label="Name">
              <span style={{ fontSize: 14, color: "var(--tx-0)" }}>{detail.name}</span>
            </Field>
            <Field label="Filename">
              <span className="mono" style={{ color: "var(--tx-2)" }}>
                {fileName(detail.path)}
              </span>
            </Field>
            <Field label="Last modified">
              <span className="mono">{detail.modified}</span>
            </Field>
            <Field label="Radios">
              <span className="mono" style={{ color: "var(--tx-0)" }}>
                {detail.radio_count} configured
              </span>
            </Field>
            <Field label="Description">
              <span style={{ color: "var(--tx-2)", textWrap: "pretty" }}>{detail.description}</span>
            </Field>
            <Field label="Author">
              <span style={{ color: "var(--tx-2)" }}>{detail.author}</span>
            </Field>
          </div>
        )}
      </div>
    </div>
  );
}

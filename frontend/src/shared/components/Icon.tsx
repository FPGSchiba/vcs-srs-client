import type { CSSProperties, ReactNode } from "react";

interface IconProps {
  name: string;
  size?: number;
  className?: string;
  style?: CSSProperties;
}

/**
 * Icon renders a thin-line stroke glyph from a fixed SVG set, ported from the
 * design prototype's `atoms.jsx` Icon component. Names map to inline SVG path
 * fragments; an unknown name renders an empty (but valid) SVG so callers never
 * crash on a typo. classNames are kept identical to the design so the ported
 * CSS applies unchanged.
 */
export function Icon({ name, size = 16, className = "", style }: IconProps) {
  const paths: Record<string, ReactNode> = {
    comms: (
      <>
        <circle cx="12" cy="12" r="2" />
        <path d="M5 5a10 10 0 0 0 0 14M19 5a10 10 0 0 1 0 14M8 8a6 6 0 0 0 0 8M16 8a6 6 0 0 1 0 8" />
      </>
    ),
    ship: (
      <>
        <path d="M3 12 L12 4 L21 12 L12 20 Z" />
        <path d="M9 12 L12 9 L15 12 L12 15 Z" />
      </>
    ),
    fleet: (
      <>
        <path d="M3 17 L12 13 L21 17" />
        <path d="M3 12 L12 8 L21 12" />
        <path d="M3 7 L12 3 L21 7" />
      </>
    ),
    users: (
      <>
        <circle cx="9" cy="9" r="3" />
        <path d="M3 19 a6 6 0 0 1 12 0" />
        <circle cx="17" cy="8" r="2.5" />
        <path d="M14 14 a5 5 0 0 1 7 5" />
      </>
    ),
    server: (
      <>
        <rect x="3" y="4" width="18" height="6" rx="1" />
        <rect x="3" y="14" width="18" height="6" rx="1" />
        <circle cx="7" cy="7" r="0.6" fill="currentColor" />
        <circle cx="7" cy="17" r="0.6" fill="currentColor" />
      </>
    ),
    admin: (
      <>
        <path d="M12 3 L20 6 V12 a8 8 0 0 1 -8 9 a8 8 0 0 1 -8 -9 V6 Z" />
        <path d="M9 12 L11 14 L15 10" />
      </>
    ),
    settings: (
      <>
        <circle cx="12" cy="12" r="3" />
        <path d="M12 3 V5 M12 19 V21 M3 12 H5 M19 12 H21 M5.6 5.6 L7 7 M17 17 L18.4 18.4 M5.6 18.4 L7 17 M17 7 L18.4 5.6" />
      </>
    ),
    help: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M9 9 a3 3 0 0 1 6 0 c0 2 -3 2 -3 4 M12 17 v0.01" />
      </>
    ),
    chat: <path d="M21 12 a9 9 0 0 1 -13 8 L3 21 L4 16 A9 9 0 1 1 21 12 Z" />,
    history: (
      <>
        <path d="M3 12 a9 9 0 1 0 3 -6.7" />
        <path d="M3 4 V8 H7" />
        <path d="M12 7 V12 L15 14" />
      </>
    ),
    bell: (
      <>
        <path d="M6 9 a6 6 0 0 1 12 0 v4 l2 3 H4 l2 -3 Z" />
        <path d="M10 19 a2 2 0 0 0 4 0" />
      </>
    ),
    chevron: <path d="M9 6 L15 12 L9 18" />,
    chevronD: <path d="M6 9 L12 15 L18 9" />,
    chevronU: <path d="M6 15 L12 9 L18 15" />,
    plus: <path d="M12 5 V19 M5 12 H19" />,
    x: <path d="M6 6 L18 18 M18 6 L6 18" />,
    edit: (
      <>
        <path d="M4 20 H8 L19 9 L15 5 L4 16 Z" />
        <path d="M13 7 L17 11" />
      </>
    ),
    save: (
      <>
        <path d="M5 4 H17 L20 7 V20 H4 V4 Z" />
        <path d="M8 4 V9 H15 V4" />
        <path d="M8 15 H16 V20 H8 Z" />
      </>
    ),
    trash: <path d="M4 7 H20 M9 7 V4 H15 V7 M6 7 L7 20 H17 L18 7" />,
    copy: (
      <>
        <rect x="8" y="8" width="12" height="12" rx="1" />
        <path d="M4 16 V4 H16 V8" />
      </>
    ),
    upload: (
      <>
        <path d="M12 4 V16 M7 9 L12 4 L17 9" />
        <path d="M4 20 H20" />
      </>
    ),
    download: (
      <>
        <path d="M12 4 V16 M7 11 L12 16 L17 11" />
        <path d="M4 20 H20" />
      </>
    ),
    folder: <path d="M3 6 H10 L12 8 H21 V19 H3 Z" />,
    search: (
      <>
        <circle cx="11" cy="11" r="6" />
        <path d="M16 16 L21 21" />
      </>
    ),
    mic: (
      <>
        <rect x="9" y="3" width="6" height="12" rx="3" />
        <path d="M5 11 a7 7 0 0 0 14 0 M12 18 V21" />
      </>
    ),
    micOff: (
      <>
        <rect x="9" y="3" width="6" height="12" rx="3" />
        <path d="M5 11 a7 7 0 0 0 14 0 M12 18 V21 M3 3 L21 21" />
      </>
    ),
    volume: <path d="M4 9 V15 H8 L13 19 V5 L8 9 Z M16 8 a5 5 0 0 1 0 8" />,
    lock: (
      <>
        <rect x="5" y="11" width="14" height="9" rx="1" />
        <path d="M8 11 V7 a4 4 0 0 1 8 0 V11" />
      </>
    ),
    unlock: (
      <>
        <rect x="5" y="11" width="14" height="9" rx="1" />
        <path d="M8 11 V7 a4 4 0 0 1 8 0" />
      </>
    ),
    layout: (
      <>
        <rect x="3" y="3" width="8" height="8" rx="1" />
        <rect x="13" y="3" width="8" height="5" rx="1" />
        <rect x="13" y="10" width="8" height="11" rx="1" />
        <rect x="3" y="13" width="8" height="8" rx="1" />
      </>
    ),
    expand: <path d="M4 9 V4 H9 M15 4 H20 V9 M20 15 V20 H15 M9 20 H4 V15" />,
    collapse: <path d="M9 4 V9 H4 M15 9 H20 V4 M20 15 H15 V20 M4 15 H9 V20" />,
    pin: (
      <>
        <path d="M9 4 H15 L14 10 L18 14 H6 L10 10 Z" />
        <path d="M12 14 V21" />
      </>
    ),
    eye: (
      <>
        <path d="M2 12 S5 5 12 5 S22 12 22 12 S19 19 12 19 S2 12 2 12 Z" />
        <circle cx="12" cy="12" r="3" />
      </>
    ),
    eyeOff: (
      <>
        <path d="M3 3 L21 21" />
        <path d="M2 12 S5 5 12 5 S22 12 22 12 S19 19 12 19 S2 12 2 12 Z" />
      </>
    ),
    refresh: (
      <>
        <path d="M20 6 V12 H14" />
        <path d="M20 12 A8 8 0 1 1 12 4 c2 0 4 1 6 2" />
      </>
    ),
    sync: <path d="M5 9 A7 7 0 0 1 18 6 M19 4 V8 H15 M19 15 A7 7 0 0 1 6 18 M5 20 V16 H9" />,
    radio: (
      <>
        <circle cx="12" cy="12" r="2" />
        <path d="M5 5a10 10 0 0 0 0 14M19 5a10 10 0 0 1 0 14" />
      </>
    ),
    knob: (
      <>
        <circle cx="12" cy="12" r="8" />
        <path d="M12 4 V8" />
      </>
    ),
    broadcast: (
      <>
        <circle cx="12" cy="12" r="2" />
        <path d="M9 9a4 4 0 0 0 0 6 M15 9a4 4 0 0 1 0 6" />
      </>
    ),
    shield: <path d="M12 3 L20 6 V12 a8 8 0 0 1 -8 9 a8 8 0 0 1 -8 -9 V6 Z" />,
    bolt: <path d="M13 3 L5 14 H11 L10 21 L19 9 H13 Z" />,
    engine: (
      <>
        <circle cx="12" cy="12" r="8" />
        <path d="M12 4 V8 M12 16 V20 M4 12 H8 M16 12 H20 M6 6 L9 9 M15 15 L18 18 M6 18 L9 15 M15 9 L18 6" />
      </>
    ),
    fuel: (
      <>
        <path d="M5 21 V5 a2 2 0 0 1 2 -2 H13 a2 2 0 0 1 2 2 V21 H5 Z" />
        <path d="M15 9 H17 L19 11 V18 a2 2 0 0 1 -4 0 V14" />
        <path d="M7 9 H13" />
      </>
    ),
    cargo: (
      <>
        <path d="M3 6 L12 3 L21 6 V18 L12 21 L3 18 Z" />
        <path d="M3 6 L12 9 L21 6" />
        <path d="M12 9 V21" />
      </>
    ),
    sensor: (
      <>
        <circle cx="12" cy="12" r="2" />
        <path d="M12 4 V7 M12 17 V20 M4 12 H7 M17 12 H20" />
      </>
    ),
    air: <path d="M3 8 H13 a3 3 0 1 0 -3 -3 M3 16 H17 a3 3 0 1 1 -3 3 M3 12 H21" />,
    quantum: (
      <>
        <circle cx="12" cy="12" r="2" />
        <ellipse cx="12" cy="12" rx="9" ry="3" />
        <ellipse cx="12" cy="12" rx="9" ry="3" transform="rotate(60 12 12)" />
        <ellipse cx="12" cy="12" rx="9" ry="3" transform="rotate(-60 12 12)" />
      </>
    ),
    weapon: (
      <>
        <path d="M3 14 H15 L17 12 L21 16 L17 20 L15 18 H3 Z" />
        <path d="M7 14 V18" />
      </>
    ),
    sos: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M9 12 L11 14 L15 10" />
      </>
    ),
    grid: (
      <>
        <rect x="3" y="3" width="7" height="7" />
        <rect x="14" y="3" width="7" height="7" />
        <rect x="3" y="14" width="7" height="7" />
        <rect x="14" y="14" width="7" height="7" />
      </>
    ),
    list: <path d="M4 6 H20 M4 12 H20 M4 18 H20" />,
    drag: (
      <>
        <circle cx="9" cy="6" r="1" fill="currentColor" />
        <circle cx="9" cy="12" r="1" fill="currentColor" />
        <circle cx="9" cy="18" r="1" fill="currentColor" />
        <circle cx="15" cy="6" r="1" fill="currentColor" />
        <circle cx="15" cy="12" r="1" fill="currentColor" />
        <circle cx="15" cy="18" r="1" fill="currentColor" />
      </>
    ),
    minimize: <path d="M5 12 H19" />,
    maximize: <rect x="5" y="5" width="14" height="14" />,
    close: <path d="M6 6 L18 18 M18 6 L6 18" />,
    info: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 11 V16 M12 8 V8.01" />
      </>
    ),
  };

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      style={style}
    >
      {paths[name] ?? null}
    </svg>
  );
}

import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./src/**/*.{ts,tsx,html}", "./*.html"],
  theme: {
    extend: {
      colors: {
        "bg-0": "var(--bg-0)",
        "bg-1": "var(--bg-1)",
        "bg-2": "var(--bg-2)",
        "bg-3": "var(--bg-3)",
        "bg-lcd": "var(--bg-lcd)",
        "bd-1": "var(--bd-1)",
        "bd-2": "var(--bd-2)",
        "bd-3": "var(--bd-3)",
        "tx-0": "var(--tx-0)",
        "tx-1": "var(--tx-1)",
        "tx-2": "var(--tx-2)",
        "tx-3": "var(--tx-3)",
        "tx-4": "var(--tx-4)",
        "ac-primary": "var(--ac-primary)",
        "ac-primary-dim": "var(--ac-primary-dim)",
        "ac-warn": "var(--ac-warn)",
        "ac-alert": "var(--ac-alert)",
        "ac-ok": "var(--ac-ok)",
        "ac-lcd": "var(--ac-lcd)",
        "ac-violet": "var(--ac-violet)",
      },
      fontFamily: {
        sans: ["Space Grotesk", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "ui-monospace", "monospace"],
      },
      borderRadius: {
        sm: "var(--r-sm)",
        md: "var(--r-md)",
        lg: "var(--r-lg)",
        pill: "var(--r-pill)",
      },
    },
  },
  plugins: [],
};

export default config;

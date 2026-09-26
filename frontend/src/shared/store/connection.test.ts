import { describe, it, expect } from "vitest";
import {
  controlDot,
  voiceDot,
  bannerVariant,
  formatRTT,
  emptyConnection,
  type ConnLink,
} from "./connection";

const link = (over: Partial<ConnLink> = {}): ConnLink => ({
  state: "connected",
  rtt_ms: 8,
  healthy: true,
  available: true,
  error: "",
  ...over,
});

describe("controlDot", () => {
  it("is ok when connected and the probe answers", () => {
    expect(controlDot(link())).toBe("ok");
  });

  it("is warn when connected but the probe has begun failing", () => {
    // One or two failed pings clear `healthy` without changing state. The
    // dot has to move before the banner does, or the only warning the user
    // gets is the banner arriving 15s later with no build-up.
    expect(controlDot(link({ healthy: false }))).toBe("warn");
  });

  it("is warn while reconnecting", () => {
    expect(controlDot(link({ state: "reconnecting", healthy: false }))).toBe("warn");
  });

  it("is alert when disconnected", () => {
    expect(controlDot(link({ state: "disconnected", healthy: false }))).toBe("alert");
  });
});

describe("voiceDot", () => {
  it("is off when unavailable", () => {
    // Not alert. Voice that never started is not voice that broke -- and on
    // every CGO-less Windows release build this is the NORMAL state.
    expect(voiceDot(link({ state: "unavailable", available: false }))).toBe("off");
  });

  it("is ok when connected", () => {
    expect(voiceDot(link())).toBe("ok");
  });

  it.each(["resolving", "handshaking", "rebinding", "retrying"])(
    "is warn while %s",
    (state) => {
      expect(voiceDot(link({ state, healthy: false }))).toBe("warn");
    },
  );

  it.each(["idle", "closed"])("is alert when %s", (state) => {
    expect(voiceDot(link({ state, healthy: false }))).toBe("alert");
  });
});

describe("bannerVariant", () => {
  it("shows nothing when both planes are healthy", () => {
    expect(bannerVariant({ server: "s", control: link(), voice: link() })).toBe("none");
  });

  it("shows control-only (VOICE DEGRADED) when voice alone is down", () => {
    expect(
      bannerVariant({
        server: "s",
        control: link(),
        voice: link({ state: "closed", healthy: false }),
      }),
    ).toBe("control-only");
  });

  it("shows voice-only (CONTROL DEGRADED) when control alone is down", () => {
    expect(
      bannerVariant({
        server: "s",
        control: link({ state: "disconnected", healthy: false }),
        voice: link(),
      }),
    ).toBe("voice-only");
  });

  it("shows disconnected when both are down", () => {
    expect(
      bannerVariant({
        server: "s",
        control: link({ state: "disconnected", healthy: false }),
        voice: link({ state: "closed", healthy: false }),
      }),
    ).toBe("disconnected");
  });

  it("ignores voice entirely when it is unavailable", () => {
    // An unavailable voice plane is not a factor in the banner: a healthy
    // control link with a stub codec must show NO banner, not a permanent
    // VOICE DEGRADED on every Windows release build.
    const unavailable = link({ state: "unavailable", available: false, healthy: false });
    expect(bannerVariant({ server: "s", control: link(), voice: unavailable })).toBe("none");
    expect(
      bannerVariant({
        server: "s",
        control: link({ state: "disconnected", healthy: false }),
        voice: unavailable,
      }),
    ).toBe("disconnected");
  });
});

describe("formatRTT", () => {
  it("renders an em dash for an unmeasured link", () => {
    // -1, not 0: voice.Session.RTT() genuinely returns 0 before the first
    // answered keepalive and after every rebind.
    expect(formatRTT(-1)).toBe("—");
  });

  it("renders milliseconds otherwise", () => {
    expect(formatRTT(0)).toBe("0ms");
    expect(formatRTT(142)).toBe("142ms");
  });
});

describe("emptyConnection", () => {
  it("is honest before anything has been measured", () => {
    const c = emptyConnection();
    expect(c.control.state).toBe("disconnected");
    expect(c.control.healthy).toBe(false);
    expect(c.voice.available).toBe(false);
    expect(c.control.rtt_ms).toBe(-1);
  });
});

/**
 * Frequency arithmetic in integer kHz.
 *
 * The wire field is a 24-bit unsigned kHz integer (internal/voice/freq.go,
 * type KHz), and the SERVER decides whether to relay a transmission by
 * comparing advertised frequencies with exact float32 equality
 * (vcs-srs-server/state/server.go). So the client keeps ONE canonical integer
 * and derives the float only at the edge: per-digit editing changes which
 * integer is added or subtracted, never the representation.
 */

export const MIN_KHZ = 0;
export const MAX_KHZ = 16_777_215; // 2^24 - 1

/** How many MHz digits are always rendered, so a digit never changes column. */
const MHZ_PAD = 3;

export function clampKhz(khz: number): number {
  return Math.min(MAX_KHZ, Math.max(MIN_KHZ, Math.round(khz)));
}

export function mhzToKhz(mhz: number): number {
  return Math.round(mhz * 1000);
}

/**
 * Mirrors internal/voice/freq.go's KHz.MHz32 EXACTLY
 * (`float32(uint32(k)) / 1000.0`). Any other rounding here silently desyncs
 * from the value the server compares against.
 */
export function khzToMhz(khz: number): number {
  return Math.fround(khz / 1000);
}

/**
 * One rendered character. `place` is the power-of-ten kHz weight the character
 * carries (0 = 1 kHz, 3 = 1 MHz), or null for the decimal separator, which is
 * not a target.
 */
export interface FreqCell {
  char: string;
  place: number | null;
}

/**
 * Splits a kHz value into display cells. The MHz part is padded to MHZ_PAD so
 * a digit keeps its column as the value changes — otherwise the digit under
 * the cursor shifts out from under it mid-scroll.
 */
export function digitsOf(khz: number): FreqCell[] {
  const k = clampKhz(khz);
  const mhzPart = String(Math.floor(k / 1000)).padStart(MHZ_PAD, "0");
  const khzPart = String(k % 1000).padStart(3, "0");
  const cells: FreqCell[] = [];
  for (let i = 0; i < mhzPart.length; i++) {
    cells.push({ char: mhzPart[i], place: mhzPart.length - 1 - i + 3 });
  }
  cells.push({ char: ".", place: null });
  for (let i = 0; i < khzPart.length; i++) {
    cells.push({ char: khzPart[i], place: khzPart.length - 1 - i });
  }
  return cells;
}

/**
 * Steps one decade. Carries and borrows fall out of plain addition, which is
 * what "a digit passing 9 bumps the digit to its left" means.
 *
 * A step that would leave the wire range is REFUSED, not clamped: clamping the
 * 100 MHz digit to MAX_KHZ would rewrite six digits the user never touched and
 * leave the radio on a frequency nobody is listening to.
 */
export function stepDigit(khz: number, place: number, dir: 1 | -1): number {
  const next = clampKhz(khz) + dir * 10 ** place;
  if (next < MIN_KHZ || next > MAX_KHZ) return clampKhz(khz);
  return next;
}

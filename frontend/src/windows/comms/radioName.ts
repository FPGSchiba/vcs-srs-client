/**
 * The single rule for renaming a radio, shared by the inline editor and the
 * settings drawer so the two paths cannot disagree: the draft is trimmed, an
 * empty (or all-whitespace) name is rejected because the card would be left
 * showing only its id, and an unchanged name is not a change.
 *
 * Returns the name to commit, or null when there is nothing to commit.
 */
export function nextRadioName(draft: string, current: string): string | null {
  const trimmed = draft.trim();
  return trimmed && trimmed !== current ? trimmed : null;
}

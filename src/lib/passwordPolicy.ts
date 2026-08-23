// Mirrors the server's minPasswordRunes floor (validatePassword, #247) so
// the three password forms can disable Submit before a round trip, rather
// than only stating the rule and letting the server catch it (#255).
export const MIN_PASSWORD_LENGTH = 8;

// [...password].length counts Unicode code points, not UTF-16 code units —
// password.length would count a surrogate-pair emoji as 2, letting a
// too-short password past this check while the server (which counts runes)
// still rejects it.
export function hasMinPasswordLength(password: string): boolean {
  return [...password].length >= MIN_PASSWORD_LENGTH;
}

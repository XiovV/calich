package service

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// connectStateTTL bounds how long a User has to complete a Provider consent
// round-trip before the "state" parameter carrying their identity expires
// (#285). Generous enough for Google's own consent screen, far short of
// being useful to replay.
const connectStateTTL = 10 * time.Minute

// connectStatePurpose is connectStateClaims' own "purpose" value — see
// accessTokenClaims' doc comment (auth_session.go) for why the claim exists.
const connectStatePurpose = "connect_state"

// connectStateClaims is the JWT payload IssueConnectState mints and
// ParseConnectState validates. Purpose distinguishes it from
// accessTokenClaims, the other claims shape this service signs under the
// same jwtSecret — without it, a real access token (a superset of these
// claims) parses as a valid connect state too, letting an attacker who has
// stolen one drive Callback with a forged authorization code.
type connectStateClaims struct {
	jwt.RegisteredClaims
	Purpose string `json:"purpose"`
}

// IssueConnectState mints a short-lived, signed token binding a Provider
// OAuth consent round-trip back to userID (#285, ADR-0051). It travels as
// the "state" query parameter Google echoes back unmodified on its redirect
// to Callback — a request that carries neither an Authorization header (the
// Access token lives in the browser's memory, not a cookie, and Google's
// redirect is a fresh top-level navigation of its own choosing) nor this
// app's own cookies reliably scoped to that path. Reusing AuthService's own
// jwtSecret rather than inventing a second signing key keeps this to two
// small methods instead of a new seam.
func (s *AuthService) IssueConnectState(userID int64) (string, error) {
	now := time.Now()
	claims := connectStateClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   formatUserID(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(connectStateTTL)),
		},
		Purpose: connectStatePurpose,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

// ParseConnectState validates a state token IssueConnectState minted and
// returns the userID it was issued for.
func (s *AuthService) ParseConnectState(state string) (int64, error) {
	claims := &connectStateClaims{}

	_, err := jwt.ParseWithClaims(state, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return 0, fmt.Errorf("parse connect state: %w", err)
	}
	if claims.Purpose != connectStatePurpose {
		return 0, errors.New("token is not a connect state")
	}

	return strconv.ParseInt(claims.Subject, 10, 64)
}

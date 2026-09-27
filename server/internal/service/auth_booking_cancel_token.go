package service

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// bookingCancelPurpose is bookingCancelClaims' own "purpose" value — see
// connectStatePurpose's doc comment (auth_connect_state.go) for why the
// claim exists: without it, any other token this service signs under the
// same jwtSecret would parse as a valid cancel token too.
const bookingCancelPurpose = "booking_cancel"

// bookingCancelClaims is the JWT payload IssueBookingCancelToken mints and
// ParseBookingCancelToken validates. Subject is the booked Event's own id —
// this is ADR-0087's "an HMAC over the Event id" — and carries no
// expiration: the link is only ever refused by Cancel checking the Event's
// own start against now, not by the token going stale on its own, so a
// visitor who waits weeks to cancel still can.
type bookingCancelClaims struct {
	jwt.RegisteredClaims
	Purpose string `json:"purpose"`
}

// IssueBookingCancelToken mints the signed cancel link a booking
// confirmation carries (#327, ADR-0087). Reuses AuthService's own
// jwtSecret rather than inventing a second signing key, mirroring
// IssueConnectState.
func (s *AuthService) IssueBookingCancelToken(eventID string) (string, error) {
	claims := bookingCancelClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: eventID},
		Purpose:          bookingCancelPurpose,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

// ParseBookingCancelToken validates a cancel token IssueBookingCancelToken
// minted and returns the Event id it was issued for.
func (s *AuthService) ParseBookingCancelToken(token string) (string, error) {
	claims := &bookingCancelClaims{}

	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse booking cancel token: %w", err)
	}
	if claims.Purpose != bookingCancelPurpose {
		return "", errors.New("token is not a booking cancel token")
	}

	return claims.Subject, nil
}

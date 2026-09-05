package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestConnectState_RoundTrips(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")

	state, err := svc.IssueConnectState(42)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	userID, err := svc.ParseConnectState(state)
	if err != nil {
		t.Fatalf("parse connect state: %v", err)
	}
	if userID != 42 {
		t.Fatalf("expected user id 42, got %d", userID)
	}
}

func TestConnectState_RejectsTampering(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")

	state, err := svc.IssueConnectState(42)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.ParseConnectState(state + "x"); err == nil {
		t.Fatalf("expected tampered state to fail to parse")
	}
}

func TestConnectState_RejectsExpired(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   formatUserID(7),
		IssuedAt:  jwt.NewNumericDate(now.Add(-connectStateTTL - time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(-time.Minute)),
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.jwtSecret)
	if err != nil {
		t.Fatalf("sign expired state: %v", err)
	}

	if _, err := svc.ParseConnectState(expired); err == nil {
		t.Fatalf("expected expired state to fail to parse")
	}
}

// TestConnectState_RejectsAccessToken is the reverse of auth_test.go's
// TestAuthenticate_RejectsConnectState: a real access token is a superset of
// connectStateClaims' fields, so without the Purpose claim it would parse as
// a valid connect state too — letting an attacker holding a stolen access
// token drive Callback with a forged authorization code and silently attach
// their own Google account as a Connection on the victim's account (#285).
func TestConnectState_RejectsAccessToken(t *testing.T) {
	svc := newTestAuthService(t, "admin", "admin")

	accessToken, err := svc.newAccessToken(42, 0)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	if _, err := svc.ParseConnectState(accessToken); err == nil {
		t.Fatalf("expected an access token to be rejected as a connect state")
	}
}

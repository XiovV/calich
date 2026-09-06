package service

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

// TestInvitationSender_Send_NilMailerFailsRatherThanPanics is the regression
// test for #290's own outbox unification: the outbox Worker now runs
// unconditionally regardless of whether SMTP is configured (ADR-0075's
// write-back needs it to drain even with no Mailer), so a stale pending
// mail-kind row — queued back when SMTP *was* configured, still pending
// after it was unset — must fail through the ordinary backoff-then-give-up
// path rather than reach a nil transport. Constructed with a bare nil
// InvitationMailer, exactly as app.go's own wiring does when a.Mailer is
// nil, never a nil-valued *mailer.SMTPMailer — the distinction this test
// exists to pin down, since the latter would panic instead of erroring.
func TestInvitationSender_Send_NilMailerFailsRatherThanPanics(t *testing.T) {
	sender := NewInvitationSender(nil, nil, "calich@example.com")

	err := sender.Send(context.Background(), repository.OutboxMessage{
		ID: 1, EventID: "evt-1", Kind: repository.OutboxKindMail, Method: repository.OutboxMethodRequest,
	})
	if !errors.Is(err, ErrNoMailTransportConfigured) {
		t.Fatalf("expected ErrNoMailTransportConfigured, got %v", err)
	}
}

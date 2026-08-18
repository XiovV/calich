package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/db"
)

func newTestSessionRepository(t *testing.T) (*SessionRepository, *UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewSessionRepository(sqlDB), NewUserRepository(sqlDB)
}

func TestSessionRepository_CreateAndGetByRefreshTokenHash(t *testing.T) {
	sessions, users := newTestSessionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	expiresAt := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	created, err := sessions.Create(ctx, user.ID, "refresh-token-hash", expiresAt)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if created.ID == 0 {
		t.Fatalf("expected non-zero session id")
	}
	if created.UserID != user.ID {
		t.Fatalf("expected user id %d, got %d", user.ID, created.UserID)
	}

	fetched, err := sessions.GetByRefreshTokenHash(ctx, "refresh-token-hash")
	if err != nil {
		t.Fatalf("get by refresh token hash: %v", err)
	}
	if fetched != created {
		t.Fatalf("expected fetched session %+v to equal created session %+v", fetched, created)
	}
}

func TestSessionRepository_GetByRefreshTokenHash_NotFound(t *testing.T) {
	sessions, _ := newTestSessionRepository(t)

	_, err := sessions.GetByRefreshTokenHash(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionRepository_Delete(t *testing.T) {
	sessions, users := newTestSessionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := sessions.Create(ctx, user.ID, "refresh-token-hash", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := sessions.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err = sessions.GetByRefreshTokenHash(ctx, "refresh-token-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSessionRepository_DeleteAllForUser(t *testing.T) {
	sessions, users := newTestSessionRepository(t)
	ctx := context.Background()

	userA, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userB, err := users.Create(ctx, "someone-else", "someone-else@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := sessions.Create(ctx, userA.ID, "session-1-for-a", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := sessions.Create(ctx, userA.ID, "session-2-for-a", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := sessions.Create(ctx, userB.ID, "session-for-b", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := sessions.DeleteAllForUser(ctx, userA.ID); err != nil {
		t.Fatalf("delete all for user: %v", err)
	}

	if _, err := sessions.GetByRefreshTokenHash(ctx, "session-1-for-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for user A's first session, got %v", err)
	}
	if _, err := sessions.GetByRefreshTokenHash(ctx, "session-2-for-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for user A's second session, got %v", err)
	}
	if _, err := sessions.GetByRefreshTokenHash(ctx, "session-for-b"); err != nil {
		t.Fatalf("expected user B's session to be untouched, got %v", err)
	}
}

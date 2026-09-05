package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/db"
)

func newTestConnectionRepository(t *testing.T) (*ConnectionRepository, *UserRepository) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewConnectionRepository(sqlDB), NewUserRepository(sqlDB)
}

func accessToken(v string) *string { return &v }

func TestConnectionRepository_Upsert_Create(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := connections.Upsert(ctx, user.ID, ProviderGoogle, "someone@gmail.com", ConnectionFields{
		AccessToken:  accessToken("access-1"),
		RefreshToken: "encrypted-refresh-1",
		Scopes:       "calendar.events calendar.calendarlist.readonly",
		Status:       ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	if created.ID == 0 {
		t.Fatalf("expected non-zero connection id")
	}
	if created.UserID != user.ID {
		t.Fatalf("expected user id %d, got %d", user.ID, created.UserID)
	}
	if created.Provider != ProviderGoogle {
		t.Fatalf("expected provider %q, got %q", ProviderGoogle, created.Provider)
	}
	if created.AccountEmail != "someone@gmail.com" {
		t.Fatalf("expected account email %q, got %q", "someone@gmail.com", created.AccountEmail)
	}
	if created.AccessToken == nil || *created.AccessToken != "access-1" {
		t.Fatalf("expected access token %q, got %v", "access-1", created.AccessToken)
	}
	if created.RefreshToken != "encrypted-refresh-1" {
		t.Fatalf("expected refresh token %q, got %q", "encrypted-refresh-1", created.RefreshToken)
	}
	if created.Status != ConnectionStatusLive {
		t.Fatalf("expected status %q, got %q", ConnectionStatusLive, created.Status)
	}
}

// TestConnectionRepository_Upsert_ReconnectSameAccount covers #285's
// acceptance criterion directly: one Connection per (User, Provider
// account), regardless of how many times the User re-authorizes it. A
// second Upsert for the same (user, provider, account_email) must replace
// the first row's tokens rather than erroring on the table's UNIQUE
// constraint or creating a duplicate.
func TestConnectionRepository_Upsert_ReconnectSameAccount(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := connections.Upsert(ctx, user.ID, ProviderGoogle, "someone@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-refresh-1",
		Scopes:       "calendar.events",
		Status:       ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := connections.Upsert(ctx, user.ID, ProviderGoogle, "someone@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-refresh-2",
		Scopes:       "calendar.events calendar.calendarlist.readonly",
		Status:       ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("expected reconnect to reuse the same connection id %d, got %d", first.ID, second.ID)
	}
	if second.RefreshToken != "encrypted-refresh-2" {
		t.Fatalf("expected refreshed token %q, got %q", "encrypted-refresh-2", second.RefreshToken)
	}

	list, err := connections.ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one connection after reconnecting, got %d", len(list))
	}
}

func TestConnectionRepository_Upsert_DistinctAccountsCoexist(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := connections.Upsert(ctx, user.ID, ProviderGoogle, "work@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-refresh-work", Status: ConnectionStatusLive,
	}); err != nil {
		t.Fatalf("upsert work connection: %v", err)
	}
	if _, err := connections.Upsert(ctx, user.ID, ProviderGoogle, "personal@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-refresh-personal", Status: ConnectionStatusLive,
	}); err != nil {
		t.Fatalf("upsert personal connection: %v", err)
	}

	list, err := connections.ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 distinct connections, got %d", len(list))
	}
}

func TestConnectionRepository_ListByUser_ScopedToOwner(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	userA, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userB, err := users.Create(ctx, "someone-else", "someone-else@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := connections.Upsert(ctx, userA.ID, ProviderGoogle, "a@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-a", Status: ConnectionStatusLive,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}
	if _, err := connections.Upsert(ctx, userB.ID, ProviderGoogle, "b@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-b", Status: ConnectionStatusLive,
	}); err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	list, err := connections.ListByUser(ctx, userA.ID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 1 || list[0].AccountEmail != "a@gmail.com" {
		t.Fatalf("expected only user A's connection, got %+v", list)
	}
}

func TestConnectionRepository_Delete(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := connections.Upsert(ctx, user.ID, ProviderGoogle, "someone@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-refresh", Status: ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	if err := connections.Delete(ctx, user.ID, created.ID); err != nil {
		t.Fatalf("delete connection: %v", err)
	}

	list, err := connections.ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no connections after delete, got %d", len(list))
	}
}

func TestConnectionRepository_Delete_NotFound(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	user, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := connections.Delete(ctx, user.ID, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestConnectionRepository_Delete_ScopedToOwner(t *testing.T) {
	connections, users := newTestConnectionRepository(t)
	ctx := context.Background()

	userA, err := users.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userB, err := users.Create(ctx, "someone-else", "someone-else@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := connections.Upsert(ctx, userA.ID, ProviderGoogle, "a@gmail.com", ConnectionFields{
		RefreshToken: "encrypted-a", Status: ConnectionStatusLive,
	})
	if err != nil {
		t.Fatalf("upsert connection: %v", err)
	}

	if err := connections.Delete(ctx, userB.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting another user's connection, got %v", err)
	}

	list, err := connections.ListByUser(ctx, userA.ID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected user A's connection to survive the other user's delete attempt, got %d remaining", len(list))
	}
}

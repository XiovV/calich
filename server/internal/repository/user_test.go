package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/XiovV/calich/server/internal/db"
)

func newTestUserRepository(t *testing.T) *UserRepository {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	return NewUserRepository(sqlDB)
}

func TestUserRepository_CreateAndGetByEmail(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hashed-password", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if created.ID == 0 {
		t.Fatalf("expected a non-zero id, got %d", created.ID)
	}
	if !created.MustChangePassword {
		t.Fatalf("expected must_change_password to be true")
	}

	fetched, err := repo.GetByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}

	if fetched != created {
		t.Fatalf("expected fetched user %+v to equal created user %+v", fetched, created)
	}
}

func TestUserRepository_GetByEmail_NotFound(t *testing.T) {
	repo := newTestUserRepository(t)

	_, err := repo.GetByEmail(context.Background(), "nobody@example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestUserRepository_Create_DuplicateEmail_DifferentCase covers #196
// (ADR-0058): users.email is COLLATE NOCASE, so the UNIQUE constraint itself
// rejects a case-differing duplicate — independent of whether the caller
// folded case before writing.
func TestUserRepository_Create_DuplicateEmail_DifferentCase(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "admin", "admin@example.com", "hash1", true); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	if _, err := repo.Create(ctx, "someone-else", "Admin@Example.com", "hash2", true); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken for a case-differing duplicate, got %v", err)
	}
}

// TestUserRepository_GetByEmail_CaseInsensitive covers #196 (ADR-0058):
// users.email's COLLATE NOCASE makes a plain "WHERE email = ?" lookup
// case-insensitive regardless of the case either side happens to carry.
func TestUserRepository_GetByEmail_CaseInsensitive(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hashed-password", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	fetched, err := repo.GetByEmail(ctx, "Admin@Example.COM")
	if err != nil {
		t.Fatalf("get by email with different case: %v", err)
	}
	if fetched != created {
		t.Fatalf("expected fetched user %+v to equal created user %+v", fetched, created)
	}
}

func TestUserRepository_Create_DuplicateEmail(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "admin", "admin@example.com", "hash1", true); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	if _, err := repo.Create(ctx, "someone-else", "admin@example.com", "hash2", true); err == nil {
		t.Fatalf("expected an error creating a duplicate email, got nil")
	}
}

func TestUserRepository_UpdatePassword(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "old-hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	updated, err := repo.UpdatePassword(ctx, created.ID, "new-hash")
	if err != nil {
		t.Fatalf("update password: %v", err)
	}

	if updated.PasswordHash != "new-hash" {
		t.Fatalf("expected password hash to be updated, got %q", updated.PasswordHash)
	}
	if updated.MustChangePassword {
		t.Fatalf("expected must_change_password to be cleared after updating password")
	}
}

// TestUserRepository_UpdatePassword_BumpsTokenVersion pins the fix for
// #242: AuthService.Authenticate checks token_version against a bearer
// access token's own claim, so a password change has to actually move this
// column for a pre-change token to stop authenticating.
func TestUserRepository_UpdatePassword_BumpsTokenVersion(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "old-hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.TokenVersion != 0 {
		t.Fatalf("expected a freshly created user to start at token_version 0, got %d", created.TokenVersion)
	}

	updated, err := repo.UpdatePassword(ctx, created.ID, "new-hash")
	if err != nil {
		t.Fatalf("update password: %v", err)
	}
	if updated.TokenVersion != created.TokenVersion+1 {
		t.Fatalf("expected token_version to be incremented by 1, got %d (was %d)", updated.TokenVersion, created.TokenVersion)
	}

	fetched, err := repo.GetTokenVersion(ctx, created.ID)
	if err != nil {
		t.Fatalf("get token version: %v", err)
	}
	if fetched != updated.TokenVersion {
		t.Fatalf("expected GetTokenVersion to reflect the update, got %d, want %d", fetched, updated.TokenVersion)
	}
}

func TestUserRepository_UpdateEmail(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.Email != "admin@example.com" {
		t.Fatalf("expected the created user's email to be set, got %+v", created.Email)
	}

	updated, err := repo.UpdateEmail(ctx, created.ID, "admin2@example.com")
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if updated.Email != "admin2@example.com" {
		t.Fatalf("expected email to be updated, got %+v", updated.Email)
	}
}

func TestUserRepository_UpdateEmail_DuplicateReturnsErrEmailTaken(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "admin", "admin@example.com", "hash", false); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	other, err := repo.Create(ctx, "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	if _, err := repo.UpdateEmail(ctx, other.ID, "admin@example.com"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestUserRepository_Count(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 users, got %d", count)
	}

	if _, err := repo.Create(ctx, "admin", "admin@example.com", "hash", true); err != nil {
		t.Fatalf("create user: %v", err)
	}

	count, err = repo.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 user, got %d", count)
	}
}

func TestUserRepository_First(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := repo.First(ctx)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first != created {
		t.Fatalf("expected first user %+v to equal created user %+v", first, created)
	}
}

func TestUserRepository_First_NotFound(t *testing.T) {
	repo := newTestUserRepository(t)

	_, err := repo.First(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_SyncedDeviceRemindersEnabled_DefaultsOff(t *testing.T) {
	repo := newTestUserRepository(t)

	created, err := repo.Create(context.Background(), "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to default off")
	}
}

func TestUserRepository_Create_DefaultsWeekStartToMonday(t *testing.T) {
	repo := newTestUserRepository(t)

	created, err := repo.Create(context.Background(), "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.WeekStart != 1 {
		t.Fatalf("expected week_start to default to 1 (Monday), got %d", created.WeekStart)
	}
}

func TestUserRepository_UpdateWeekStart(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	updated, err := repo.UpdateWeekStart(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("update week start: %v", err)
	}
	if updated.WeekStart != 0 {
		t.Fatalf("expected week_start 0, got %d", updated.WeekStart)
	}
}

func TestUserRepository_Create_DefaultsDefaultViewToWeek(t *testing.T) {
	repo := newTestUserRepository(t)

	created, err := repo.Create(context.Background(), "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.DefaultView != "week" {
		t.Fatalf("expected default_view to default to \"week\", got %q", created.DefaultView)
	}
}

func TestUserRepository_UpdateDefaultView(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	updated, err := repo.UpdateDefaultView(ctx, created.ID, "month")
	if err != nil {
		t.Fatalf("update default view: %v", err)
	}
	if updated.DefaultView != "month" {
		t.Fatalf("expected default_view \"month\", got %q", updated.DefaultView)
	}
}

func TestUserRepository_Create_DuplicateEmail_ReturnsErrEmailTaken(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "admin", "admin@example.com", "hash1", true); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	if _, err := repo.Create(ctx, "someone-else", "admin@example.com", "hash2", true); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// TestUserRepository_TwoUsersMayShareTheSameName covers ADR-0047: Name is a
// display label, not an identifier, so two accounts may hold the same one —
// only Email is required to be unique.
func TestUserRepository_TwoUsersMayShareTheSameName(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, "Jane Smith", "jane1@example.com", "hash1", true); err != nil {
		t.Fatalf("create first jane: %v", err)
	}

	if _, err := repo.Create(ctx, "Jane Smith", "jane2@example.com", "hash2", true); err != nil {
		t.Fatalf("expected two accounts to share a name, got: %v", err)
	}
}

func TestUserRepository_SetDisabled(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "alice", "alice@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.IsDisabled {
		t.Fatalf("expected a freshly created user to not be disabled")
	}

	disabled, err := repo.SetDisabled(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	if !disabled.IsDisabled {
		t.Fatalf("expected user to be disabled")
	}

	enabled, err := repo.SetDisabled(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if enabled.IsDisabled {
		t.Fatalf("expected user to no longer be disabled")
	}
}

func TestUserRepository_Delete(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "alice", "alice@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestUserRepository_Delete_NotFound(t *testing.T) {
	repo := newTestUserRepository(t)

	if err := repo.Delete(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestUserRepository_Delete_CascadesOwnedCalendarsAndSharesGrantedToThem
// covers ADR-0037's DispositionDelete: deleting a User removes the
// Calendars they owned (via calendars.user_id ON DELETE CASCADE) and, with
// them, the Shares granted on those Calendars — and separately, the Shares
// granted *to* the deleted User themselves (calendar_shares.user_id ON
// DELETE CASCADE), regardless of disposition.
func TestUserRepository_Delete_CascadesOwnedCalendarsAndSharesGrantedToThem(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := users.Create(ctx, "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	// The Workspace is owned by a separate User: Workspace ownership blocks
	// deletion (ADR-0044's sole-Owner guard — owner_user_id has no ON DELETE
	// behaviour), and this test is about a Calendar Owner's deletion cascade,
	// not Workspace ownership, so "owner" is only a Member here.
	workspaceOwner, err := users.Create(ctx, "workspace-owner", "workspace-owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create workspace owner: %v", err)
	}
	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", workspaceOwner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, workspaceOwner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace owner member: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, WorkspaceRoleMember); err != nil {
		t.Fatalf("add owner as workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	if _, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarFields{Name: "Family", Color: "blue"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	shares := NewCalendarShareRepository(sqlDB)
	// "other" holds a Share both on the owner's Calendar (should vanish when
	// the Calendar cascades away) and, separately, is the recipient of a
	// Share granted to them directly that has nothing to do with owner's
	// deletion.
	if _, err := shares.Upsert(ctx, "cal-1", other.ID, RoleEditor); err != nil {
		t.Fatalf("share cal-1 with other: %v", err)
	}

	if err := users.Delete(ctx, owner.ID); err != nil {
		t.Fatalf("delete owner: %v", err)
	}

	if _, err := calendars.GetByIDAny(ctx, "cal-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cal-1 to cascade-delete with its owner, got %v", err)
	}
	if _, err := shares.Get(ctx, "cal-1", other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected other's share on the deleted calendar to cascade away, got %v", err)
	}
}

// TestUserRepository_Delete_CascadesSharesGrantedToTheDeletedUser proves the
// other direction of ADR-0037's "Shares granted to the deleted User are
// removed": a Share held on someone else's Calendar disappears when the
// holder is deleted, even though that Calendar isn't theirs and is
// untouched otherwise.
func TestUserRepository_Delete_CascadesSharesGrantedToTheDeletedUser(t *testing.T) {
	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	ctx := context.Background()

	users := NewUserRepository(sqlDB)
	owner, err := users.Create(ctx, "owner", "owner@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	holder, err := users.Create(ctx, "holder", "holder@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create holder: %v", err)
	}

	workspaces := NewWorkspaceRepository(sqlDB)
	workspace, err := workspaces.Create(ctx, "workspace-a", owner.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaces.AddMember(ctx, workspace.ID, owner.ID, WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendars := NewCalendarRepository(sqlDB)
	if _, err := calendars.Create(ctx, owner.ID, workspace.ID, "cal-1", CalendarFields{Name: "Family", Color: "blue"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	shares := NewCalendarShareRepository(sqlDB)
	if _, err := shares.Upsert(ctx, "cal-1", holder.ID, RoleEditor); err != nil {
		t.Fatalf("share cal-1 with holder: %v", err)
	}

	if err := users.Delete(ctx, holder.ID); err != nil {
		t.Fatalf("delete holder: %v", err)
	}

	if _, err := shares.Get(ctx, "cal-1", holder.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected holder's share to be removed, got %v", err)
	}
	if _, err := calendars.GetByIDAny(ctx, "cal-1"); err != nil {
		t.Fatalf("expected the owner's calendar to survive, got %v", err)
	}
}

func TestUserRepository_ListEnabledExcluding(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	caller, err := repo.Create(ctx, "caller", "caller@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}
	bob, err := repo.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	alice, err := repo.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	disabled, err := repo.Create(ctx, "ghost", "ghost@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create ghost: %v", err)
	}
	if _, err := repo.SetDisabled(ctx, disabled.ID, true); err != nil {
		t.Fatalf("disable ghost: %v", err)
	}

	users, err := repo.ListEnabledExcluding(ctx, caller.ID)
	if err != nil {
		t.Fatalf("list enabled excluding: %v", err)
	}
	if len(users) != 2 || users[0] != alice || users[1] != bob {
		t.Fatalf("expected [alice, bob] ordered by name, got %+v", users)
	}
}

func TestUserRepository_ListEnabledExcluding_Empty(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	caller, err := repo.Create(ctx, "caller", "caller@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	users, err := repo.ListEnabledExcluding(ctx, caller.ID)
	if err != nil {
		t.Fatalf("list enabled excluding: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no users, got %+v", users)
	}
}

func TestUserRepository_UpdateSyncedDeviceReminders(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	updated, err := repo.UpdateSyncedDeviceReminders(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("update synced device reminders: %v", err)
	}
	if !updated.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to be enabled")
	}

	reverted, err := repo.UpdateSyncedDeviceReminders(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("update synced device reminders: %v", err)
	}
	if reverted.SyncedDeviceRemindersEnabled {
		t.Fatalf("expected synced device reminders to be disabled again")
	}
}

// TestUserRepository_GetByIDs is #118's batched lookup, backing Event
// creator attribution: one query for every distinct id, with a deleted or
// unknown id simply absent from the result rather than erroring.
func TestUserRepository_GetByIDs(t *testing.T) {
	repo := newTestUserRepository(t)
	ctx := context.Background()

	alice, err := repo.Create(ctx, "alice", "alice@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := repo.Create(ctx, "bob", "bob@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	const deletedID = int64(999)
	users, err := repo.GetByIDs(ctx, []int64{alice.ID, bob.ID, deletedID})
	if err != nil {
		t.Fatalf("get by ids: %v", err)
	}
	if len(users) != 2 || users[alice.ID] != alice || users[bob.ID] != bob {
		t.Fatalf("expected alice and bob keyed by id, got %+v", users)
	}
	if _, ok := users[deletedID]; ok {
		t.Fatalf("expected no entry for an id with no matching row")
	}
}

func TestUserRepository_GetByIDs_Empty(t *testing.T) {
	repo := newTestUserRepository(t)

	users, err := repo.GetByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("get by ids: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no users, got %+v", users)
	}
}

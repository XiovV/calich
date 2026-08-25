package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WorkspaceInvite is a single-use, hashed, expiring invite for an email to
// join a Workspace (ADR-0044) — mirroring ADR-0042's account-level Invite
// mechanics, but keyed by (workspace_id, email) rather than living on a User
// row, since the invited address may not have a User yet.
type WorkspaceInvite struct {
	ID              int64
	WorkspaceID     int64
	Email           string
	InviteTokenHash string
	InviteExpiresAt time.Time
	CreatedAt       time.Time
}

// ErrWorkspaceInviteExists is returned by Create when workspaceID already has
// an outstanding invite for email — the caller should use SetTokenHash to
// reissue it instead of creating a second, competing one.
var ErrWorkspaceInviteExists = errors.New("workspace already has an outstanding invite for this email")

const workspaceInviteColumns = `id, workspace_id, email, invite_token_hash, invite_expires_at, created_at`

type WorkspaceInviteRepository struct {
	db DBTX
}

func NewWorkspaceInviteRepository(db *sql.DB) *WorkspaceInviteRepository {
	return &WorkspaceInviteRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018) — accepting
// an invite for a brand-new account needs this to pair the new User row and
// its owning WorkspaceMember row with consuming the invite in one
// transaction.
func (r *WorkspaceInviteRepository) WithTx(tx *sql.Tx) *WorkspaceInviteRepository {
	return &WorkspaceInviteRepository{db: tx}
}

// Create inserts a new WorkspaceInvite for email in workspaceID. Fails with
// ErrWorkspaceInviteExists if one is already outstanding for that pair — the
// caller should reissue it (SetTokenHash) rather than create a second one.
func (r *WorkspaceInviteRepository) Create(ctx context.Context, workspaceID int64, email, inviteTokenHash string, inviteExpiresAt time.Time) (WorkspaceInvite, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO workspace_invites (workspace_id, email, invite_token_hash, invite_expires_at) VALUES (?, ?, ?, ?)`,
		workspaceID, email, inviteTokenHash, inviteExpiresAt.UTC(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return WorkspaceInvite{}, ErrWorkspaceInviteExists
		}
		return WorkspaceInvite{}, fmt.Errorf("insert workspace invite: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return WorkspaceInvite{}, fmt.Errorf("get inserted workspace invite id: %w", err)
	}

	return r.GetByID(ctx, id)
}

func (r *WorkspaceInviteRepository) GetByID(ctx context.Context, id int64) (WorkspaceInvite, error) {
	return r.scanInvite(r.db.QueryRowContext(ctx,
		`SELECT `+workspaceInviteColumns+` FROM workspace_invites WHERE id = ?`, id,
	))
}

// ListByWorkspace returns every outstanding WorkspaceInvite for workspaceID,
// ordered by when it was issued — the member-management screen's outstanding-
// invites list (#165), shown alongside active Members.
func (r *WorkspaceInviteRepository) ListByWorkspace(ctx context.Context, workspaceID int64) ([]WorkspaceInvite, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+workspaceInviteColumns+` FROM workspace_invites WHERE workspace_id = ? ORDER BY created_at, id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list workspace invites: %w", err)
	}
	return collectRows(rows, scanWorkspaceInviteRow)
}

// GetByTokenHash looks up the WorkspaceInvite a token hashes to — used by
// AuthService, which only ever has the hash, never an id.
func (r *WorkspaceInviteRepository) GetByTokenHash(ctx context.Context, inviteTokenHash string) (WorkspaceInvite, error) {
	return r.scanInvite(r.db.QueryRowContext(ctx,
		`SELECT `+workspaceInviteColumns+` FROM workspace_invites WHERE invite_token_hash = ?`, inviteTokenHash,
	))
}

// SetTokenHash overwrites id's outstanding invite with a new token hash and
// expiry (reissue) — the previous token stops matching GetByTokenHash the
// moment this commits, mirroring UserRepository.SetInvite.
func (r *WorkspaceInviteRepository) SetTokenHash(ctx context.Context, id int64, inviteTokenHash string, inviteExpiresAt time.Time) (WorkspaceInvite, error) {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE workspace_invites SET invite_token_hash = ?, invite_expires_at = ? WHERE id = ?`,
		inviteTokenHash, inviteExpiresAt.UTC(), id,
	); err != nil {
		return WorkspaceInvite{}, fmt.Errorf("set workspace invite token: %w", err)
	}
	return r.GetByID(ctx, id)
}

// Delete removes a WorkspaceInvite outright — called once its token has been
// consumed by an accept path, since an invite is single-use.
func (r *WorkspaceInviteRepository) Delete(ctx context.Context, id int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM workspace_invites WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete workspace invite: %w", err)
	}
	return nil
}

func (r *WorkspaceInviteRepository) scanInvite(row *sql.Row) (WorkspaceInvite, error) {
	i, err := scanWorkspaceInviteRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkspaceInvite{}, ErrNotFound
		}
		return WorkspaceInvite{}, fmt.Errorf("scan workspace invite: %w", err)
	}
	return i, nil
}

func scanWorkspaceInviteRow(row rowScanner) (WorkspaceInvite, error) {
	var i WorkspaceInvite
	if err := row.Scan(&i.ID, &i.WorkspaceID, &i.Email, &i.InviteTokenHash, &i.InviteExpiresAt, &i.CreatedAt); err != nil {
		return WorkspaceInvite{}, err
	}
	return i, nil
}

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type AppPassword struct {
	ID         int64
	UserID     int64
	Label      string
	Hash       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type AppPasswordRepository struct {
	db *sql.DB
}

func NewAppPasswordRepository(db *sql.DB) *AppPasswordRepository {
	return &AppPasswordRepository{db: db}
}

func (r *AppPasswordRepository) Create(ctx context.Context, userID int64, label, hash string) (AppPassword, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO app_passwords (user_id, label, hash) VALUES (?, ?, ?)`,
		userID, label, hash,
	)
	if err != nil {
		return AppPassword{}, fmt.Errorf("insert app password: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return AppPassword{}, fmt.Errorf("get inserted app password id: %w", err)
	}

	return r.getByID(ctx, id)
}

// ListForUser returns userID's app passwords, most recently created first.
func (r *AppPasswordRepository) ListForUser(ctx context.Context, userID int64) ([]AppPassword, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, label, hash, created_at, last_used_at FROM app_passwords WHERE user_id = ? ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list app passwords: %w", err)
	}
	defer rows.Close()

	var appPasswords []AppPassword
	for rows.Next() {
		appPassword, err := scanAppPassword(rows)
		if err != nil {
			return nil, fmt.Errorf("scan app password: %w", err)
		}
		appPasswords = append(appPasswords, appPassword)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app passwords: %w", err)
	}

	return appPasswords, nil
}

// Delete removes userID's app password with the given id. It returns
// ErrNotFound if no such app password belongs to that user, so a user can
// never revoke someone else's credential.
func (r *AppPasswordRepository) Delete(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM app_passwords WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete app password: %w", err)
	}

	return requireAffected(res)
}

// UpdateLastUsedAt stamps userID's app password id's last_used_at with the
// current time — called after a successful CalDAV Basic-auth check
// (ADR-0024). Scoped by user_id like Delete, so a caller can never stamp
// another user's app password even by mistake.
func (r *AppPasswordRepository) UpdateLastUsedAt(ctx context.Context, userID, id int64) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE app_passwords SET last_used_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return fmt.Errorf("update app password last used at: %w", err)
	}
	return nil
}

func (r *AppPasswordRepository) getByID(ctx context.Context, id int64) (AppPassword, error) {
	return scanAppPassword(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, label, hash, created_at, last_used_at FROM app_passwords WHERE id = ?`, id,
	))
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAppPassword(row rowScanner) (AppPassword, error) {
	var p AppPassword
	var lastUsedAt sql.NullTime
	err := row.Scan(&p.ID, &p.UserID, &p.Label, &p.Hash, &p.CreatedAt, &lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AppPassword{}, ErrNotFound
	}
	if err != nil {
		return AppPassword{}, fmt.Errorf("scan app password: %w", err)
	}
	if lastUsedAt.Valid {
		p.LastUsedAt = &lastUsedAt.Time
	}
	return p, nil
}

// connection.go implements connections (#285, ADR-0052): one User's
// authorized grant to one account at one Provider — the tokens, that
// account's Email, its granted scopes, and whether it is live, expired or
// revoked. Belongs to the User rather than to any Workspace (one grant
// serves every Workspace they're in); a calendar_sources row of kind
// 'connection' points back to one of these (a later ticket's).
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Provider discriminates which external service a Connection authorizes
// against. Google is the only one this app speaks to today (ADR-0050);
// Microsoft is the one the Provider seam exists to make cheap later.
type Provider string

const ProviderGoogle Provider = "google"

// ConnectionStatus is whether a Connection's grant is currently usable.
// Live is the only status this app itself ever sets (#285); Expired and
// Revoked are later tickets' (a refresh failure, a token Google reports as
// no longer valid) — drawn now so the column and its CHECK constraint don't
// need revisiting when those land.
type ConnectionStatus string

const (
	ConnectionStatusLive    ConnectionStatus = "live"
	ConnectionStatusExpired ConnectionStatus = "expired"
	ConnectionStatusRevoked ConnectionStatus = "revoked"
)

// Connection is one User's authorized link to one account at one Provider
// (#285, ADR-0052).
type Connection struct {
	ID           int64
	UserID       int64
	Provider     Provider
	AccountEmail string
	// AccessToken is short-lived and never relied on across requests without
	// a refresh — nil until the first successful token exchange populates it
	// (there isn't one yet: this app has no caller that needs it before a
	// later ticket's Provider calls).
	AccessToken *string
	// RefreshToken is encrypted at rest with a key from the environment,
	// never stored in DATA_DIR (ADR-0052) — ConnectionService encrypts
	// before Upsert and decrypts after a read, so this column always holds
	// ciphertext, never a raw token.
	RefreshToken string
	// Scopes is the space-separated OAuth scope string the Provider actually
	// granted, exactly as it answered — not merely what was requested.
	Scopes    string
	Status    ConnectionStatus
	CreatedAt time.Time
}

// ConnectionFields are a Connection's columns set by Upsert — everything
// except the identity triple (UserID, Provider, AccountEmail) Upsert already
// takes as its own arguments.
type ConnectionFields struct {
	AccessToken  *string
	RefreshToken string
	Scopes       string
	Status       ConnectionStatus
}

type ConnectionRepository struct {
	db DBTX
}

func NewConnectionRepository(db *sql.DB) *ConnectionRepository {
	return &ConnectionRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *ConnectionRepository) WithTx(tx *sql.Tx) *ConnectionRepository {
	return &ConnectionRepository{db: tx}
}

const connectionColumns = `id, user_id, provider, account_email, access_token, refresh_token, scopes, status, created_at`

// Upsert creates userID's Connection to (provider, accountEmail), or — if one
// already exists — replaces its tokens, scopes and status in place. This is
// what keeps "one Connection per (User, Provider account)" true (#285,
// ADR-0052) without the caller having to look one up first: re-authorizing
// an account already connected (a repeat consent, or recovering an expired
// grant) reuses the same row and id rather than erroring on the table's
// UNIQUE constraint or leaving a duplicate.
func (r *ConnectionRepository) Upsert(ctx context.Context, userID int64, provider Provider, accountEmail string, fields ConnectionFields) (Connection, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO connections (user_id, provider, account_email, access_token, refresh_token, scopes, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, provider, account_email) DO UPDATE SET
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			scopes = excluded.scopes,
			status = excluded.status`,
		userID, provider, accountEmail, fields.AccessToken, fields.RefreshToken, fields.Scopes, fields.Status,
	); err != nil {
		return Connection{}, fmt.Errorf("upsert connection: %w", err)
	}

	return r.getByProviderAccount(ctx, userID, provider, accountEmail)
}

// ListByUser returns userID's Connections, most recently created first.
func (r *ConnectionRepository) ListByUser(ctx context.Context, userID int64) ([]Connection, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+connectionColumns+` FROM connections WHERE user_id = ? ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	return collectRows(rows, scanConnection)
}

// Delete removes userID's Connection with the given id. It returns
// ErrNotFound if no such Connection belongs to that user, so a User can
// never disconnect someone else's grant.
func (r *ConnectionRepository) Delete(ctx context.Context, userID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM connections WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	return requireAffected(res)
}

func (r *ConnectionRepository) getByProviderAccount(ctx context.Context, userID int64, provider Provider, accountEmail string) (Connection, error) {
	return scanConnection(r.db.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM connections WHERE user_id = ? AND provider = ? AND account_email = ?`,
		userID, provider, accountEmail,
	))
}

func scanConnection(row rowScanner) (Connection, error) {
	var c Connection
	err := row.Scan(&c.ID, &c.UserID, &c.Provider, &c.AccountEmail, &c.AccessToken, &c.RefreshToken, &c.Scopes, &c.Status, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, fmt.Errorf("scan connection: %w", err)
	}
	return c, nil
}

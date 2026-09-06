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
// Callback sets Live on every successful (re-)authorization (#285);
// mintAccessToken sets Expired the moment a stored refresh_token no longer
// mints a fresh access token (#291, ADR-0075's "including ADR-0051's
// seven-day Testing-status trap") — Google's own OAuth error doesn't
// distinguish a token it expired from one a User explicitly revoked, so this
// app never sets Revoked itself; the value exists for that distinction if a
// future signal can ever name it specifically.
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

// GetByID returns userID's Connection with the given id, or ErrNotFound if
// no such Connection belongs to them — the Calendar picker's (#286) way of
// resolving which account to call Google as, scoped by user the same way
// Delete already is so a caller can never reach someone else's grant.
func (r *ConnectionRepository) GetByID(ctx context.Context, userID, id int64) (Connection, error) {
	return scanConnection(r.db.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM connections WHERE id = ? AND user_id = ?`,
		id, userID,
	))
}

// ListByIDs returns every one of ids' Connections, keyed by id — deliberately
// unscoped by user, unlike GetByID/Delete: its only caller is the sidebar's
// per-Connection heading join (#286), a read-only display lookup where the
// Calendar already visible to the caller is what established this data is
// theirs to see, and it is never used to authorize a write.
func (r *ConnectionRepository) ListByIDs(ctx context.Context, ids []int64) (map[int64]Connection, error) {
	result := make(map[int64]Connection, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+connectionColumns+` FROM connections WHERE id IN (`+placeholders(len(ids))+`)`, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list connections by ids: %w", err)
	}
	connections, err := collectRows(rows, scanConnection)
	if err != nil {
		return nil, err
	}
	for _, c := range connections {
		result[c.ID] = c
	}
	return result, nil
}

// UpdateAccessToken replaces id's stored access_token alone (#287) — a Full
// Refresh's own token-refresh helper persists what refreshAccessToken just
// minted here, so a Refresh moments later within the same access token's
// lifetime doesn't have to mint another. refresh_token, scopes and status
// are untouched.
func (r *ConnectionRepository) UpdateAccessToken(ctx context.Context, userID, id int64, accessToken string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE connections SET access_token = ? WHERE id = ? AND user_id = ?`,
		accessToken, id, userID,
	)
	if err != nil {
		return fmt.Errorf("update access token: %w", err)
	}
	return requireAffected(res)
}

// UpdateStatus replaces id's stored status alone (#291, ADR-0075) — the
// counterpart to UpdateAccessToken for the column a dead refresh_token
// actually needs to move: a Full/Delta Refresh or a Write-back push that
// discovers the grant no longer authenticates (mintAccessToken's own
// refreshAccessToken failing) records that here, so an expired or revoked
// Connection stops accepting new edits (EventService.requireLiveConnection)
// until a User reconnects. access_token, refresh_token and scopes are
// untouched — Upsert's own ON CONFLICT is what actually replaces those, on
// reconnect.
func (r *ConnectionRepository) UpdateStatus(ctx context.Context, userID, id int64, status ConnectionStatus) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE connections SET status = ? WHERE id = ? AND user_id = ?`,
		status, id, userID,
	)
	if err != nil {
		return fmt.Errorf("update connection status: %w", err)
	}
	return requireAffected(res)
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

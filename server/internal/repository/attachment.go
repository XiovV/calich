package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Attachment is a file held against a Master, shown on every Occurrence of
// its series (#132, ADR-0040). ID is also its filename on disk and its RFC
// 8607 MANAGED-ID — minted server-side only, never accepted from a client.
type Attachment struct {
	ID          string
	EventID     string
	Filename    string
	ContentType string
	SizeBytes   int64
	// UploadedBy is who uploaded this Attachment, for attribution only —
	// never consulted for authorization, which resolves through the Event's
	// Calendar instead (ADR-0034), mirroring events.created_by. Nil when the
	// uploading User has since been deleted (ON DELETE SET NULL).
	UploadedBy *int64
	// UploadedByUsername is UploadedBy's Username, for display. Not a
	// column — populated by the service layer from users, mirroring
	// Event.CreatedByUsername. Empty whenever UploadedBy is nil.
	UploadedByUsername string
	CreatedAt          time.Time
}

type AttachmentRepository struct {
	db DBTX
}

func NewAttachmentRepository(db *sql.DB) *AttachmentRepository {
	return &AttachmentRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *AttachmentRepository) WithTx(tx *sql.Tx) *AttachmentRepository {
	return &AttachmentRepository{db: tx}
}

func (r *AttachmentRepository) Create(ctx context.Context, id, eventID string, uploadedBy *int64, filename, contentType string, sizeBytes int64) (Attachment, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO event_attachments (id, event_id, filename, content_type, size_bytes, uploaded_by) VALUES (?, ?, ?, ?, ?, ?)`,
		id, eventID, filename, contentType, sizeBytes, uploadedBy,
	); err != nil {
		return Attachment{}, fmt.Errorf("insert attachment: %w", err)
	}

	return r.GetByID(ctx, id)
}

func (r *AttachmentRepository) GetByID(ctx context.Context, id string) (Attachment, error) {
	return scanAttachment(r.db.QueryRowContext(ctx,
		`SELECT id, event_id, filename, content_type, size_bytes, uploaded_by, created_at FROM event_attachments WHERE id = ?`,
		id,
	))
}

// CountByEventID reports how many Attachments eventID already carries, for
// enforcing MAX_ATTACHMENTS_PER_EVENT (ADR-0040).
func (r *AttachmentRepository) CountByEventID(ctx context.Context, eventID string) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM event_attachments WHERE event_id = ?`,
		eventID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count attachments: %w", err)
	}
	return count, nil
}

// ListByEventIDs returns each event's Attachments, keyed by event_id,
// ordered by created_at so a list renders in upload order. Events with no
// Attachments are simply absent from the map. Mirrors
// EventReminderRepository.ListByEventIDs.
func (r *AttachmentRepository) ListByEventIDs(ctx context.Context, eventIDs []string) (map[string][]Attachment, error) {
	result := make(map[string][]Attachment)
	if len(eventIDs) == 0 {
		return result, nil
	}

	query := `SELECT id, event_id, filename, content_type, size_bytes, uploaded_by, created_at FROM event_attachments WHERE event_id IN (` + placeholders(len(eventIDs)) + `) ORDER BY created_at, id`
	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		a, err := scanAttachmentRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		result[a.EventID] = append(result[a.EventID], a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments: %w", err)
	}

	return result, nil
}

// ListAllIDs returns every Attachment id this instance's database currently
// knows about — the sweeper's "known" set (ADR-0040): a file on disk whose
// id isn't in this list is garbage from a crashed upload or a cascade
// delete no Go code observed file-by-file.
func (r *AttachmentRepository) ListAllIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM event_attachments`)
	if err != nil {
		return nil, fmt.Errorf("list attachment ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan attachment id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment ids: %w", err)
	}
	return ids, nil
}

// Delete removes id's row. The caller unlinks the file only after this
// commits (ADR-0040) — see attachmentstore.Store.Delete.
func (r *AttachmentRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM event_attachments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	return requireAffected(res)
}

func scanAttachment(row *sql.Row) (Attachment, error) {
	a, err := scanAttachmentRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attachment{}, ErrNotFound
		}
		return Attachment{}, fmt.Errorf("scan attachment: %w", err)
	}
	return a, nil
}

func scanAttachmentRow(row rowScanner) (Attachment, error) {
	var a Attachment
	if err := row.Scan(&a.ID, &a.EventID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt); err != nil {
		return Attachment{}, err
	}
	return a, nil
}

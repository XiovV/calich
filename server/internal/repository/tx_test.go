package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	sqlDB := repo.db.(*sql.DB)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	err := WithTx(ctx, sqlDB, func(tx *sql.Tx) error {
		_, err := repo.WithTx(tx).Create(ctx, "evt-1", userID, calendarID, "Standup", start, end, false, "", nil, nil)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "evt-1"); err != nil {
		t.Fatalf("expected event visible after commit, got: %v", err)
	}
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	repo, userID, calendarID, _ := newTestEventRepository(t)
	sqlDB := repo.db.(*sql.DB)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	sentinel := errors.New("boom")

	err := WithTx(ctx, sqlDB, func(tx *sql.Tx) error {
		if _, err := repo.WithTx(tx).Create(ctx, "evt-1", userID, calendarID, "Standup", start, end, false, "", nil, nil); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}

	if _, err := repo.GetByID(ctx, userID, "evt-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected event absent after rollback, got: %v", err)
	}
}

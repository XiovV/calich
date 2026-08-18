package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/repository"
)

// AttachmentSweeper reclaims Attachment bytes with no matching row (#132,
// ADR-0040) — the mechanism, not a safety net: a crash between Save and its
// row commit, and every cascade delete (calendar, series, account) that
// removes event_attachments rows inside SQLite with no Go code watching
// file-by-file, both leave orphaned files only this catches. Same shape as
// Poller: an injected Tick, and Run looping it — except Run also ticks once
// immediately, since files can already be orphaned at startup.
type AttachmentSweeper struct {
	attachments *repository.AttachmentRepository
	store       *attachmentstore.Store
}

func NewAttachmentSweeper(attachments *repository.AttachmentRepository, store *attachmentstore.Store) *AttachmentSweeper {
	return &AttachmentSweeper{attachments: attachments, store: store}
}

// Tick sweeps once: every file under the attachments directory whose id has
// no event_attachments row is removed.
func (s *AttachmentSweeper) Tick(ctx context.Context) error {
	ids, err := s.attachments.ListAllIDs(ctx)
	if err != nil {
		return fmt.Errorf("list attachment ids: %w", err)
	}

	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}

	if err := s.store.Sweep(known); err != nil {
		return fmt.Errorf("sweep attachment store: %w", err)
	}
	return nil
}

// Run ticks once immediately (startup), then every interval until ctx is
// cancelled (the daily tick, ADR-0040).
func (s *AttachmentSweeper) Run(ctx context.Context, interval time.Duration) {
	if err := s.Tick(ctx); err != nil {
		log.Printf("attachment sweeper tick: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				log.Printf("attachment sweeper tick: %v", err)
			}
		}
	}
}

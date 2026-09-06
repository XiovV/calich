-- +goose Up
-- Write-back conflict, failure, and reconnect (#291, ADR-0075, ADR-0076):
-- write_back_error is the per-Event marker a permanently failed Write-back
-- push leaves behind — a conflict that survived three refetch-and-retry
-- attempts, a 403 on a calendar whose write access Google has revoked, or an
-- outbox message that exhausted its own backoff schedule against a
-- Connection that no longer authenticates. NULL while healthy.
--
-- Deliberately not cleared by an ordinary Refresh succeeding: a Refresh
-- reading the calendar back says nothing about whether this app's own
-- write ever reached it (ADR-0076's "the per-Event failure marker is the
-- user's only chance to act"). Only a fresh edit re-enqueuing a push
-- (EventRepository.ClearWriteBackError) or a push that finally lands
-- (EventRepository.UpdateProviderEtag, applied as a Refresh result) removes
-- it.
ALTER TABLE events ADD COLUMN write_back_error TEXT;

-- +goose Down
ALTER TABLE events DROP COLUMN write_back_error;

package repository

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound is returned by any repository read or write that addresses a
// row by (user, id) and finds none — whether because it never existed or
// because it belongs to another user. Callers cannot tell those apart, which
// is deliberate: it stops one user probing for another's row ids.
var ErrNotFound = errors.New("not found")

// requireAffected turns "the statement matched no rows" into ErrNotFound.
// Every UPDATE and DELETE in this package is scoped by user_id as well as id,
// so a zero count means the row is absent or someone else's.
//
// Not for statements where a zero count is a legitimate outcome rather than a
// failure — see FiredReminderRepository.MarkFired, whose INSERT OR IGNORE
// reports affected == 0 to mean "already fired" (ADR-0021).
func requireAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

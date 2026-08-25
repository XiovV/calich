package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/XiovV/calich/server/internal/repository"
)

// Disposition values a CalendarDisposition can name — there is no default
// (ADR-0037, ADR-0044): guessing wrong either silently destroys a shared
// Calendar or hands its data to someone with no business holding it. Shared
// by AccountService.Delete (subjectUserID is the deleting User themselves)
// and WorkspaceService.RemoveMember (subjectUserID is the Member being
// removed) — the same disposition mechanic applied at two different scopes,
// whole-account and single-Workspace (ADR-0044).
const (
	DispositionTransfer = "transfer"
	DispositionDelete   = "delete"
)

var (
	// ErrInvalidDisposition is returned when a CalendarDisposition names
	// neither DispositionTransfer nor DispositionDelete — there is no
	// default (ADR-0037).
	ErrInvalidDisposition = errors.New(`disposition must be "transfer" or "delete"`)
	// ErrTransferTargetRequired is returned when a CalendarDisposition is
	// DispositionTransfer with no TransferTo.
	ErrTransferTargetRequired = errors.New("transfer_to is required when disposition is \"transfer\"")
	// ErrCannotTransferToSubject is returned when TransferTo names the User
	// whose owned Calendars are being disposed of — the deleting User
	// themselves (AccountService.Delete) or the Member being removed
	// (WorkspaceService.RemoveMember) — who can't receive a transfer since
	// their standing as that Calendar's Owner is exactly what's ending.
	ErrCannotTransferToSubject = errors.New("cannot transfer a calendar to the user whose calendars are being disposed of")
	// ErrTransferTargetNotWorkspaceMember is returned when TransferTo
	// doesn't belong to the Calendar's own Workspace — a Calendar's Owner
	// must belong to its Workspace (ADR-0044, ADR-0045), so a transfer
	// target from anywhere else, or that doesn't exist at all, is refused
	// the same way.
	ErrTransferTargetNotWorkspaceMember = errors.New("transfer target must be a member of the calendar's workspace")
	// ErrCalendarNotOwned is returned when a CalendarDisposition names a
	// Calendar not owned, within the relevant scope, by the User whose
	// Calendars are being disposed of.
	ErrCalendarNotOwned = errors.New("calendar is not owned by the user whose calendars are being disposed of")
	// ErrDuplicateDisposition is returned when the same Calendar appears
	// more than once across the dispositions given.
	ErrDuplicateDisposition = errors.New("a calendar cannot appear more than once")
	// ErrMissingDisposition is returned when a disposition wasn't given for
	// every Calendar in scope — self-Delete and removal both require an
	// explicit transfer-or-delete choice for each one (ADR-0044), so a
	// Calendar can't be left silently uncovered.
	ErrMissingDisposition = errors.New("every owned calendar needs a disposition")
)

// TransferCandidate is one other Member of a Calendar's Workspace — a valid
// transfer target for that Calendar, since a Calendar's Owner must belong to
// its own Workspace (ADR-0044, ADR-0045).
type TransferCandidate struct {
	ID   int64
	Name string
}

// CalendarImpact is one Calendar in scope for a transfer-or-delete
// disposition: which Workspace it belongs to, how many Users hold a Share on
// it and would therefore lose Access under DispositionDelete, and who it
// could be transferred to instead.
type CalendarImpact struct {
	ID                 string
	Name               string
	WorkspaceID        int64
	WorkspaceName      string
	ShareCount         int
	TransferCandidates []TransferCandidate
}

// CalendarDisposition is one owned Calendar's transfer-or-delete choice
// (ADR-0044) — self-Delete and removal both require one per Calendar in
// scope, unlike the retired Admin-driven Delete, which took a single
// disposition for the whole account.
type CalendarDisposition struct {
	CalendarID  string
	Disposition string // DispositionTransfer or DispositionDelete
	// TransferTo is required, and must name a current Member of the
	// Calendar's own Workspace, when Disposition is DispositionTransfer.
	TransferTo *int64
}

// isWorkspaceMember reports whether userID belongs to workspaceID —
// validateDispositions' check on a transfer target, abstracted so
// AccountService and WorkspaceService can each supply their own
// IsMember/WorkspaceService.IsMember without this file depending on either.
type isWorkspaceMember func(ctx context.Context, workspaceID, userID int64) (bool, error)

// validateDispositions checks dispositions against calendars — the
// Calendars subjectUserID owns in the relevant scope (every Workspace, for
// AccountService.Delete; one Workspace, for WorkspaceService.RemoveMember):
// every Calendar named at most once (ErrDuplicateDisposition), every named
// Calendar actually in calendars (ErrCalendarNotOwned), a recognized
// Disposition (ErrInvalidDisposition), a transfer target that isn't
// subjectUserID (ErrCannotTransferToSubject) and belongs to the Calendar's
// own Workspace (ErrTransferTargetRequired, ErrTransferTargetNotWorkspaceMember),
// and a disposition present for every Calendar in calendars
// (ErrMissingDisposition). Callers apply dispositions only once this
// returns nil.
func validateDispositions(ctx context.Context, calendars []repository.Calendar, dispositions []CalendarDisposition, subjectUserID int64, isMember isWorkspaceMember) error {
	byID := make(map[string]repository.Calendar, len(calendars))
	for _, c := range calendars {
		byID[c.ID] = c
	}

	seen := make(map[string]bool, len(dispositions))
	for _, d := range dispositions {
		if seen[d.CalendarID] {
			return ErrDuplicateDisposition
		}
		seen[d.CalendarID] = true

		calendar, ok := byID[d.CalendarID]
		if !ok {
			return ErrCalendarNotOwned
		}

		switch d.Disposition {
		case DispositionTransfer:
			if d.TransferTo == nil {
				return ErrTransferTargetRequired
			}
			if *d.TransferTo == subjectUserID {
				return ErrCannotTransferToSubject
			}
			member, err := isMember(ctx, calendar.WorkspaceID, *d.TransferTo)
			if err != nil {
				return err
			}
			if !member {
				return ErrTransferTargetNotWorkspaceMember
			}
		case DispositionDelete:
		default:
			return ErrInvalidDisposition
		}
	}
	if len(seen) != len(calendars) {
		return ErrMissingDisposition
	}
	return nil
}

// applyDispositions applies each of dispositions inside tx — transferring or
// deleting subjectUserID's ownership of the named Calendar per its
// Disposition. Callers must have already run validateDispositions against
// the same calendars/dispositions; this trusts that without re-checking.
func applyDispositions(ctx context.Context, tx *sql.Tx, calendarRepo *repository.CalendarRepository, subjectUserID int64, dispositions []CalendarDisposition) error {
	txCalendars := calendarRepo.WithTx(tx)
	for _, d := range dispositions {
		switch d.Disposition {
		case DispositionTransfer:
			if err := txCalendars.TransferOwnershipOne(ctx, subjectUserID, d.CalendarID, *d.TransferTo); err != nil {
				return fmt.Errorf("transfer calendar %s: %w", d.CalendarID, err)
			}
		case DispositionDelete:
			if err := txCalendars.Delete(ctx, subjectUserID, d.CalendarID); err != nil {
				return fmt.Errorf("delete calendar %s: %w", d.CalendarID, err)
			}
		}
	}
	return nil
}

// calendarImpact builds calendar's CalendarImpact — its Share count and the
// transfer candidates already resolved for its Workspace — the per-Calendar
// body DeleteImpact and RemoveMemberImpact each finish with once they know
// the Calendar's Workspace name and transfer candidates.
func calendarImpact(ctx context.Context, shareRepo *repository.CalendarShareRepository, calendar repository.Calendar, workspaceName string, candidates []TransferCandidate) (CalendarImpact, error) {
	shares, err := shareRepo.ListByCalendarWithUser(ctx, calendar.ID)
	if err != nil {
		return CalendarImpact{}, fmt.Errorf("list shares for calendar %s: %w", calendar.ID, err)
	}
	return CalendarImpact{
		ID:                 calendar.ID,
		Name:               calendar.Name,
		WorkspaceID:        calendar.WorkspaceID,
		WorkspaceName:      workspaceName,
		ShareCount:         len(shares),
		TransferCandidates: candidates,
	}, nil
}

package handlers

import "github.com/XiovV/calich/server/internal/service"

// transferCandidateResponse, calendarImpactResponse and deleteImpactResponse
// are shared by AccountHandler.DeleteImpact and WorkspaceHandler.
// RemoveMemberImpact — both preview the same transfer-or-delete disposition
// mechanic (ADR-0044), just at a different scope, whole-account vs. a single
// Workspace.
type transferCandidateResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type calendarImpactResponse struct {
	ID                 string                      `json:"id"`
	Name               string                      `json:"name"`
	WorkspaceID        int64                       `json:"workspace_id"`
	WorkspaceName      string                      `json:"workspace_name"`
	ShareCount         int                         `json:"share_count"`
	TransferCandidates []transferCandidateResponse `json:"transfer_candidates"`
}

type deleteImpactResponse struct {
	Calendars []calendarImpactResponse `json:"calendars"`
}

// toDeleteImpactResponse renders calendars — service.DeleteImpact's or
// service.RemoveMemberImpact's Calendars field — as the response body both
// handlers send.
func toDeleteImpactResponse(calendars []service.CalendarImpact) deleteImpactResponse {
	responses := make([]calendarImpactResponse, len(calendars))
	for i, c := range calendars {
		candidates := make([]transferCandidateResponse, len(c.TransferCandidates))
		for j, candidate := range c.TransferCandidates {
			candidates[j] = transferCandidateResponse{ID: candidate.ID, Name: candidate.Name}
		}
		responses[i] = calendarImpactResponse{
			ID:                 c.ID,
			Name:               c.Name,
			WorkspaceID:        c.WorkspaceID,
			WorkspaceName:      c.WorkspaceName,
			ShareCount:         c.ShareCount,
			TransferCandidates: candidates,
		}
	}
	return deleteImpactResponse{Calendars: responses}
}

// calendarDispositionRequest is one Calendar's transfer-or-delete choice
// (ADR-0044), decoded from a deleteAccountRequest's or removeMemberRequest's
// Calendars field.
type calendarDispositionRequest struct {
	CalendarID string `json:"calendar_id"`
	// Disposition is "transfer" or "delete" (ADR-0044). There is no default:
	// every Calendar in scope needs one, named explicitly.
	Disposition string `json:"disposition"`
	// TransferTo is required, and must name a current Member of the
	// Calendar's own Workspace, when Disposition is "transfer".
	TransferTo *int64 `json:"transfer_to,omitempty"`
}

// toCalendarDispositions converts requests — a deleteAccountRequest's or
// removeMemberRequest's Calendars field — into the service.CalendarDisposition
// slice AccountService.Delete and WorkspaceService.RemoveMember both take.
func toCalendarDispositions(requests []calendarDispositionRequest) []service.CalendarDisposition {
	dispositions := make([]service.CalendarDisposition, len(requests))
	for i, c := range requests {
		dispositions[i] = service.CalendarDisposition{
			CalendarID:  c.CalendarID,
			Disposition: c.Disposition,
			TransferTo:  c.TransferTo,
		}
	}
	return dispositions
}

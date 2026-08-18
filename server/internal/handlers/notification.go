package handlers

import (
	"net/http"
	"time"

	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

type NotificationHandler struct {
	notifications *service.NotificationService
}

func NewNotificationHandler(notifications *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{notifications: notifications}
}

// notificationResponse is a Notification's wire shape (ADR-0021, ADR-0061)
// — the in-app feed panel's list item. OccurrenceStart is omitted for an
// invite Notification, which concerns the Event series rather than one
// Occurrence.
type notificationResponse struct {
	ID              int64   `json:"id"`
	EventID         string  `json:"eventId"`
	Kind            string  `json:"kind"`
	Title           string  `json:"title"`
	OccurrenceStart *string `json:"occurrenceStart,omitempty"`
	FiredAt         string  `json:"firedAt"`
	Seen            bool    `json:"seen"`
}

func toNotificationResponse(n repository.Notification) notificationResponse {
	var occurrenceStart *string
	if n.OccurrenceStart != nil {
		formatted := n.OccurrenceStart.UTC().Format(time.RFC3339Nano)
		occurrenceStart = &formatted
	}

	return notificationResponse{
		ID:              n.ID,
		EventID:         n.EventID,
		Kind:            n.Kind,
		Title:           n.Title,
		OccurrenceStart: occurrenceStart,
		FiredAt:         n.FiredAt.UTC().Format(time.RFC3339Nano),
		Seen:            n.Seen,
	}
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	notifications, err := h.notifications.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list notifications")
		return
	}

	response := make([]notificationResponse, len(notifications))
	for i, n := range notifications {
		response[i] = toNotificationResponse(n)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}

func (h *NotificationHandler) MarkSeen(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	if err := h.notifications.MarkAllSeen(r.Context(), userID); err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to mark notifications seen")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

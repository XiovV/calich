package service

import (
	"context"
	"fmt"

	"github.com/XiovV/calendar/server/internal/repository"
)

// notificationFeedLimit caps how many recent Notifications the feed panel
// shows — generous enough for "recent" without unbounded growth.
const notificationFeedLimit = 50

type NotificationService struct {
	notifications *repository.NotificationRepository
}

func NewNotificationService(notifications *repository.NotificationRepository) *NotificationService {
	return &NotificationService{notifications: notifications}
}

// List returns userID's most recent Notifications, newest first.
func (s *NotificationService) List(ctx context.Context, userID int64) ([]repository.Notification, error) {
	notifications, err := s.notifications.ListRecentByUser(ctx, userID, notificationFeedLimit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return notifications, nil
}

// MarkAllSeen marks every one of userID's currently-unseen Notifications as
// seen (opening the feed panel).
func (s *NotificationService) MarkAllSeen(ctx context.Context, userID int64) error {
	if err := s.notifications.MarkAllSeen(ctx, userID); err != nil {
		return fmt.Errorf("mark notifications seen: %w", err)
	}
	return nil
}

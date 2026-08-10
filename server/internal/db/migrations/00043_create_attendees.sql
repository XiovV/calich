-- +goose Up
-- attendees is an Attendee (ADR-0046): a User invited to one specific Event,
-- independent of Calendar Access — the invite itself is the grant, scoped
-- to that Event alone. response is the iCalendar PARTSTAT the Attendee has
-- set on their own invite, defaulting to needs-action until they respond.
CREATE TABLE attendees (
    event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    response TEXT NOT NULL DEFAULT 'needs-action' CHECK (response IN ('needs-action', 'accepted', 'declined', 'tentative')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (event_id, user_id)
);

-- Drives "which Events is this User an Attendee of" — the visible-events
-- union's reverse-lookup direction.
CREATE INDEX idx_attendees_user_id ON attendees(user_id);

-- +goose Down
DROP TABLE attendees;

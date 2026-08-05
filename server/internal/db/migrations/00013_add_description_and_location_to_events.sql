-- +goose Up
-- description and location are free-text fields on an Event, viewable and
-- editable in the web app now so the CalDAV serializer has them to emit
-- later (#60). Additive and non-destructive: every pre-existing row keeps
-- both as NULL, rendered as empty.
ALTER TABLE events ADD COLUMN description TEXT;
ALTER TABLE events ADD COLUMN location TEXT;

-- +goose Down
ALTER TABLE events DROP COLUMN description;
ALTER TABLE events DROP COLUMN location;

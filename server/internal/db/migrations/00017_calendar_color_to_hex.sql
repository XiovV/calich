-- +goose Up
-- ADR-0029: a Calendar's color is now an arbitrary sRGB value, not a choice
-- from the fixed 8-value enum. Existing rows still hold an enum member
-- name; migrate each in place to its former canonical hex (calendarColorHex
-- in the now-deleted caldavserver/color.go), so nothing renders as an
-- unrecognized color after the cutover.
UPDATE calendars SET color = '#E2483DFF' WHERE color = 'tomato';
UPDATE calendars SET color = '#E67C9AFF' WHERE color = 'flamingo';
UPDATE calendars SET color = '#E4C441FF' WHERE color = 'banana';
UPDATE calendars SET color = '#6B9071FF' WHERE color = 'sage';
UPDATE calendars SET color = '#12809CFF' WHERE color = 'peacock';
UPDATE calendars SET color = '#3F51B5FF' WHERE color = 'blueberry';
UPDATE calendars SET color = '#8E44ADFF' WHERE color = 'grape';
UPDATE calendars SET color = '#6B7280FF' WHERE color = 'graphite';

-- +goose Down
UPDATE calendars SET color = 'tomato' WHERE color = '#E2483DFF';
UPDATE calendars SET color = 'flamingo' WHERE color = '#E67C9AFF';
UPDATE calendars SET color = 'banana' WHERE color = '#E4C441FF';
UPDATE calendars SET color = 'sage' WHERE color = '#6B9071FF';
UPDATE calendars SET color = 'peacock' WHERE color = '#12809CFF';
UPDATE calendars SET color = 'blueberry' WHERE color = '#3F51B5FF';
UPDATE calendars SET color = 'grape' WHERE color = '#8E44ADFF';
UPDATE calendars SET color = 'graphite' WHERE color = '#6B7280FF';

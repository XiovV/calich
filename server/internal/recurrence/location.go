package recurrence

import (
	"fmt"
	"time"
)

// ResolveLocation maps an Event's tzid anchor (ADR-0019) onto a
// *time.Location: a named IANA zone resolves to that zone, "Etc/UTC"
// resolves to UTC, and nil (a Floating Event) also resolves to UTC — the
// server has no Viewer zone to expand in, and floating instants are stored
// as literal wall-clock components regardless of Location (see
// newDateTimeProp).
func ResolveLocation(tzid *string) (*time.Location, error) {
	if tzid == nil {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(*tzid)
	if err != nil {
		return nil, fmt.Errorf("load location %q: %w", *tzid, err)
	}
	return loc, nil
}

// ctag.go injects the Apple/DAVx⁵ CS:getctag property into PROPFIND
// responses for Calendar collections (ADR-0025, #65), via propertyinject.go's
// shared XML-patching mechanism (also used for calendar-color, ADR-0028):
// the library already emits an empty <getctag></getctag> (or, defensively, a
// self-closed <getctag/>) in a 404 propstat for any unknown property, so
// patching means finding that fragment and replacing it with a real value
// in a 200 propstat. Dispatch (deciding when to patch, and running exactly
// one recorder pass per request) lives in propfind.go.
package caldavserver

import "context"

const getctagNamespace = "http://calendarserver.org/ns/"

// injectGetCTag rewrites every <response> block in body whose href resolves
// via ctagFor to replace its empty, 404 <getctag/> with a 200 propstat
// carrying the real value. Blocks ctagFor declines (not a Calendar
// collection, or no getctag present) are returned unchanged.
func injectGetCTag(ctx context.Context, body []byte, ctagFor propertyValueFunc) []byte {
	return injectProperty(ctx, body, "getctag", getctagNamespace, ctagFor)
}

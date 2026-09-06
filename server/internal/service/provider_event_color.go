// provider_event_color.go implements #289's until-touched rule for a Linked
// Calendar's per-Event colour — the same shadow mechanism ADR-0032 applies to
// a Subscription's calendar name (resolveFollowedField, subscribe.go),
// applied instead to what the Google mapper seeds from an Event's own
// colorId (ADR-0075). Pure and table-tested directly, like the rest of the
// reconciler.
//
// Connection-only: a Subscription has no per-Event Provider colour to track
// (its ProviderColor is always nil), so these functions are applied solely on
// the Connection Refresh path — reconcileAgainstStored's followEventColor
// argument for Full Refresh, and mergeDeltaChange unconditionally for Delta
// Refresh, which only ever runs against a Connection-kind Source.
package service

// resolveFollowedEventColor applies the until-touched rule to one Event's
// colour: currentDisplay is the colour shown now (nil = inherit the
// Calendar's, ADR-0043), currentShadow is the Provider colour it was last
// seeded from or compared against (nil = never seeded yet), and
// providerColor is what this Refresh just mapped from the Provider's own
// colorId (nil = the Provider's event carries no colorId, or one this app
// doesn't recognise). Returns the colour to display and the shadow to store.
//
//   - currentShadow is nil, or currentDisplay still equals it: the Event is
//     untouched (or has never been seeded) — track the Provider's current
//     colour in both, including back to nil when the Provider now offers
//     none.
//   - otherwise: the Event has been recoloured here — keep currentDisplay,
//     but still record what the Provider now shows, so a later coincidental
//     match resumes tracking. The comparison above is the entire
//     "overridden" flag; there is no separate one (ADR-0032, ADR-0075).
func resolveFollowedEventColor(currentDisplay, currentShadow, providerColor *string) (display, shadow *string) {
	if currentShadow == nil || colorEqual(currentDisplay, currentShadow) {
		return providerColor, providerColor
	}
	return currentDisplay, providerColor
}

// followSeriesEventColor rewrites incoming's own Master colour and every
// Override's colour it shares with existing (matched by RecurrenceID) under
// resolveFollowedEventColor, given what's already stored (existing) and what
// this fetch just mapped from the Provider (incoming) — whose Color and
// ProviderColor already equal the Provider's current colour verbatim, the
// mapper's own seeding. An incoming Override with no stored counterpart (a
// newly modified instance) is left exactly as the mapper produced it: there
// is nothing stored yet to have diverged from.
func followSeriesEventColor(existing, incoming SeriesWrite) SeriesWrite {
	incoming.Color, incoming.ProviderColor = resolveFollowedEventColor(existing.Color, existing.ProviderColor, incoming.ProviderColor)

	existingByRecurrenceID := overridesByRecurrenceID(existing.Overrides)

	overrides := make([]OverrideWrite, len(incoming.Overrides))
	for i, o := range incoming.Overrides {
		if match, ok := existingByRecurrenceID[o.RecurrenceID.UnixNano()]; ok {
			o.Color, o.ProviderColor = resolveFollowedEventColor(match.Color, match.ProviderColor, o.ProviderColor)
		}
		overrides[i] = o
	}
	incoming.Overrides = overrides

	return incoming
}

// followEventColorAcrossSeries applies followSeriesEventColor to every
// incoming series matched to an existing one by ExternalUID — a Full
// Refresh's own pre-pass over ReconcileSeries' input (reconcileAgainstStored),
// so the diff it computes already reflects the until-touched rule rather than
// clobbering a local recolour or mistaking an untouched Event's Provider
// recolour for no change at all. A series with no stored counterpart (a
// brand new one) is left exactly as the mapper produced it — Color and
// ProviderColor both the Provider's current colour, the correct seed.
func followEventColorAcrossSeries(existing []ExistingSeries, incoming []IncomingSeries) []IncomingSeries {
	existingByUID := make(map[string]ExistingSeries, len(existing))
	for _, e := range existing {
		existingByUID[e.ExternalUID] = e
	}

	out := make([]IncomingSeries, len(incoming))
	for i, in := range incoming {
		if e, ok := existingByUID[in.ExternalUID]; ok {
			in.Write = followSeriesEventColor(e.Content, in.Write)
		}
		out[i] = in
	}
	return out
}

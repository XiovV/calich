package service

import "testing"

func TestResolveFollowedEventColor_NeverSeededTracksProvider(t *testing.T) {
	display, shadow := resolveFollowedEventColor(nil, nil, strPtr("#FBD75BFF"))
	if display == nil || *display != "#FBD75BFF" {
		t.Fatalf("expected an unseeded Event to adopt the Provider colour, got %v", display)
	}
	if shadow == nil || *shadow != "#FBD75BFF" {
		t.Fatalf("expected the shadow to be seeded to the Provider colour, got %v", shadow)
	}
}

func TestResolveFollowedEventColor_UntouchedFollowsProviderRecolour(t *testing.T) {
	// currentDisplay still equals currentShadow: untouched.
	display, shadow := resolveFollowedEventColor(strPtr("#FBD75BFF"), strPtr("#FBD75BFF"), strPtr("#DC2127FF"))
	if display == nil || *display != "#DC2127FF" {
		t.Fatalf("expected the untouched Event to follow the Provider's recolour, got %v", display)
	}
	if shadow == nil || *shadow != "#DC2127FF" {
		t.Fatalf("expected the shadow to track the new Provider colour, got %v", shadow)
	}
}

func TestResolveFollowedEventColor_UntouchedFollowsProviderDroppingItsColour(t *testing.T) {
	// The Provider now supplies no colorId at all — an untouched Event
	// reverts to inheriting the Calendar's colour, exactly as following any
	// other Provider recolour would.
	display, shadow := resolveFollowedEventColor(strPtr("#FBD75BFF"), strPtr("#FBD75BFF"), nil)
	if display != nil {
		t.Fatalf("expected the untouched Event to revert to inherit, got %v", *display)
	}
	if shadow != nil {
		t.Fatalf("expected the shadow to track the Provider's now-absent colour, got %v", *shadow)
	}
}

func TestResolveFollowedEventColor_RecolouredHereSurvivesProviderColourUnchanged(t *testing.T) {
	display, shadow := resolveFollowedEventColor(strPtr("#123456FF"), strPtr("#FBD75BFF"), strPtr("#FBD75BFF"))
	if display == nil || *display != "#123456FF" {
		t.Fatalf("expected the local recolour to survive an unchanged Provider colour, got %v", display)
	}
	if shadow == nil || *shadow != "#FBD75BFF" {
		t.Fatalf("expected the shadow to still track the Provider's (unchanged) colour, got %v", shadow)
	}
}

func TestResolveFollowedEventColor_RecolouredHereKeepsItsColourAcrossAProviderRecolour(t *testing.T) {
	// The User picked their own colour, diverged from the shadow — a later
	// Provider recolour still doesn't reach the display, but the shadow moves
	// so a later coincidental match would resume tracking.
	display, shadow := resolveFollowedEventColor(strPtr("#123456FF"), strPtr("#FBD75BFF"), strPtr("#DC2127FF"))
	if display == nil || *display != "#123456FF" {
		t.Fatalf("expected the customized colour to survive the Provider's recolour, got %v", display)
	}
	if shadow == nil || *shadow != "#DC2127FF" {
		t.Fatalf("expected the shadow to still record what the Provider now shows, got %v", shadow)
	}
}

func TestResolveFollowedEventColor_MatchingShadowAgainResumesTracking(t *testing.T) {
	// The comparison between currentDisplay and currentShadow is the entire
	// "overridden" flag — if a later Provider colour happens to coincide with
	// what's displayed, tracking resumes with no separate flag to clear.
	display, shadow := resolveFollowedEventColor(strPtr("#DC2127FF"), strPtr("#DC2127FF"), strPtr("#46D6DBFF"))
	if display == nil || *display != "#46D6DBFF" {
		t.Fatalf("expected tracking to resume once display coincidentally matched the shadow, got %v", display)
	}
	if shadow == nil || *shadow != "#46D6DBFF" {
		t.Fatalf("expected the shadow to move to the new Provider colour, got %v", shadow)
	}
}

func TestFollowSeriesEventColor_MasterFollowsUntouched(t *testing.T) {
	existing := SeriesWrite{Color: strPtr("#FBD75BFF"), ProviderColor: strPtr("#FBD75BFF")}
	incoming := SeriesWrite{Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF")}

	got := followSeriesEventColor(existing, incoming)

	if got.Color == nil || *got.Color != "#DC2127FF" {
		t.Fatalf("expected the Master's untouched colour to follow the Provider, got %v", got.Color)
	}
	if got.ProviderColor == nil || *got.ProviderColor != "#DC2127FF" {
		t.Fatalf("expected the Master's shadow to move, got %v", got.ProviderColor)
	}
}

func TestFollowSeriesEventColor_MasterRecolouredHereSurvives(t *testing.T) {
	existing := SeriesWrite{Color: strPtr("#123456FF"), ProviderColor: strPtr("#FBD75BFF")}
	incoming := SeriesWrite{Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF")}

	got := followSeriesEventColor(existing, incoming)

	if got.Color == nil || *got.Color != "#123456FF" {
		t.Fatalf("expected the Master's local recolour to survive, got %v", got.Color)
	}
	if got.ProviderColor == nil || *got.ProviderColor != "#DC2127FF" {
		t.Fatalf("expected the Master's shadow to still move, got %v", got.ProviderColor)
	}
}

func TestFollowSeriesEventColor_MatchedOverrideAppliesTheSameRule(t *testing.T) {
	recurrenceID := mustTime(t, "2026-01-12T14:00:00Z")
	existing := SeriesWrite{
		Overrides: []OverrideWrite{{RecurrenceID: recurrenceID, Color: strPtr("#123456FF"), ProviderColor: strPtr("#FBD75BFF")}},
	}
	incoming := SeriesWrite{
		Overrides: []OverrideWrite{{RecurrenceID: recurrenceID, Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF")}},
	}

	got := followSeriesEventColor(existing, incoming)

	if len(got.Overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(got.Overrides))
	}
	if got.Overrides[0].Color == nil || *got.Overrides[0].Color != "#123456FF" {
		t.Fatalf("expected the Override's local recolour to survive, got %v", got.Overrides[0].Color)
	}
	if got.Overrides[0].ProviderColor == nil || *got.Overrides[0].ProviderColor != "#DC2127FF" {
		t.Fatalf("expected the Override's shadow to still move, got %v", got.Overrides[0].ProviderColor)
	}
}

func TestFollowSeriesEventColor_NewOverrideIsLeftAsTheMapperSeededIt(t *testing.T) {
	incoming := SeriesWrite{
		Overrides: []OverrideWrite{{RecurrenceID: mustTime(t, "2026-01-12T14:00:00Z"), Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF")}},
	}

	got := followSeriesEventColor(SeriesWrite{}, incoming)

	if len(got.Overrides) != 1 || got.Overrides[0].Color == nil || *got.Overrides[0].Color != "#DC2127FF" {
		t.Fatalf("expected a brand new override to keep the Provider's seeded colour, got %+v", got.Overrides)
	}
}

func TestFollowEventColorAcrossSeries_NewSeriesIsLeftAsTheMapperSeededIt(t *testing.T) {
	incoming := []IncomingSeries{{ExternalUID: "evt-1", Write: SeriesWrite{Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF")}}}

	got := followEventColorAcrossSeries(nil, incoming)

	if len(got) != 1 || got[0].Write.Color == nil || *got[0].Write.Color != "#DC2127FF" {
		t.Fatalf("expected a brand new series to keep the Provider's seeded colour, got %+v", got)
	}
}

func TestFollowEventColorAcrossSeries_MatchedSeriesIsResolvedAgainstExisting(t *testing.T) {
	existing := []ExistingSeries{{ExternalUID: "evt-1", Content: SeriesWrite{Color: strPtr("#123456FF"), ProviderColor: strPtr("#FBD75BFF")}}}
	incoming := []IncomingSeries{{ExternalUID: "evt-1", Write: SeriesWrite{Color: strPtr("#DC2127FF"), ProviderColor: strPtr("#DC2127FF")}}}

	got := followEventColorAcrossSeries(existing, incoming)

	if len(got) != 1 || got[0].Write.Color == nil || *got[0].Write.Color != "#123456FF" {
		t.Fatalf("expected the matched series' local recolour to survive, got %+v", got)
	}
	if got[0].Write.ProviderColor == nil || *got[0].Write.ProviderColor != "#DC2127FF" {
		t.Fatalf("expected the matched series' shadow to still move, got %+v", got)
	}
}

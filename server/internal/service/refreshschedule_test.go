package service

import (
	"errors"
	"testing"
	"time"
)

func TestEffectiveRefreshInterval(t *testing.T) {
	hour := int((time.Hour).Seconds())
	twelveHours := int((12 * time.Hour).Seconds())
	tenMinutes := int((10 * time.Minute).Seconds())

	cases := []struct {
		name      string
		publisher *int
		want      time.Duration
	}{
		{"no publisher interval falls back to default", nil, time.Hour},
		{"publisher interval longer than default is honoured", &twelveHours, 12 * time.Hour},
		{"publisher interval shorter than default never speeds things up", &tenMinutes, time.Hour},
		{"publisher interval equal to default", &hour, time.Hour},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectiveRefreshInterval(time.Hour, c.publisher)
			if got != c.want {
				t.Fatalf("expected %v, got %v", c.want, got)
			}
		})
	}
}

func TestBackoffInterval(t *testing.T) {
	base := time.Hour

	cases := []struct {
		failureCount int
		want         time.Duration
	}{
		{1, time.Hour},
		{2, 2 * time.Hour},
		{3, 4 * time.Hour},
		{5, 16 * time.Hour},
		{6, MaxRefreshBackoff},
		{100, MaxRefreshBackoff},
	}

	for _, c := range cases {
		got := backoffInterval(base, c.failureCount)
		if got != c.want {
			t.Fatalf("failureCount=%d: expected %v, got %v", c.failureCount, c.want, got)
		}
	}
}

func TestBackoffInterval_NeverExceedsCeilingEvenWithALongBase(t *testing.T) {
	got := backoffInterval(20*time.Hour, 3)
	if got != MaxRefreshBackoff {
		t.Fatalf("expected the ceiling to cap a large base doubled, got %v", got)
	}
}

func TestStaggerJitter_DeterministicAndBounded(t *testing.T) {
	interval := time.Hour

	first := staggerJitter("calendar-a", interval)
	second := staggerJitter("calendar-a", interval)
	if first != second {
		t.Fatalf("expected staggerJitter to be deterministic for the same id, got %v and %v", first, second)
	}
	if first < 0 || first >= interval/4 {
		t.Fatalf("expected jitter to be within a quarter of the interval, got %v", first)
	}

	other := staggerJitter("calendar-b", interval)
	if other == first {
		t.Fatalf("expected two different calendar ids to plausibly get different jitter (got equal by chance: %v)", first)
	}
}

func TestNextRefreshTime_AddsOffsetAndStagger(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := time.Hour

	got := nextRefreshTime(now, "cal-1", base, base)
	want := now.Add(base).Add(staggerJitter("cal-1", base))
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestClassifyRefreshError(t *testing.T) {
	cases := []struct {
		err       error
		wantClass string
	}{
		{ErrSubscribeAuthFailed, ErrorClassNeedsAttention},
		{ErrSubscribeNotFound, ErrorClassNeedsAttention},
		{ErrSubscribeUnparseable, ErrorClassNeedsAttention},
		{ErrSubscribeTooLarge, ErrorClassNeedsAttention},
		{ErrSubscribeFetchFailed, ErrorClassRetrying},
		{errors.New("some other transient error"), ErrorClassRetrying},
	}

	for _, c := range cases {
		gotClass, gotMessage := classifyRefreshError(c.err)
		if gotClass != c.wantClass {
			t.Fatalf("error %v: expected class %q, got %q", c.err, c.wantClass, gotClass)
		}
		if gotMessage != c.err.Error() {
			t.Fatalf("expected message %q, got %q", c.err.Error(), gotMessage)
		}
	}
}

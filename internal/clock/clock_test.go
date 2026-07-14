package clock_test

import (
	"testing"
	"time"

	"github.com/michaeltansy/billing-engine/internal/clock"
)

var dueDate = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

func overdue(c clock.Clock, due time.Time) bool {
	return due.Before(clock.Today(c))
}

func TestOverdueBoundary(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "one second before the due day begins",
			now:  time.Date(2026, 7, 19, 23, 59, 59, 0, time.UTC),
			want: false,
		},
		{
			name: "the instant the due day begins",
			now:  time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "last payable second of the due day",
			now:  time.Date(2026, 7, 20, 23, 59, 59, 0, time.UTC),
			want: false,
		},
		{
			name: "first second of the day after due",
			now:  time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "well past due",
			now:  time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := overdue(clock.NewFake(tc.now), dueDate)
			if got != tc.want {
				t.Errorf("overdue(now=%s, due=%s) = %v, want %v",
					tc.now.Format(time.RFC3339), dueDate.Format("2006-01-02"), got, tc.want)
			}
		})
	}
}

func TestFakeNormalisesToUTC(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*60*60)
	c := clock.NewFake(time.Date(2026, 7, 21, 6, 30, 0, 0, jakarta))

	if got := clock.Today(c); !got.Equal(dueDate) {
		t.Errorf("Today() = %s, want %s", got.Format(time.RFC3339), dueDate.Format(time.RFC3339))
	}
	if overdue(c, dueDate) {
		t.Error("overdue = true, want false: 23:30 UTC is still the due day")
	}
}

func TestSystemClockIsUTC(t *testing.T) {
	if loc := (clock.System{}).Now().Location(); loc != time.UTC {
		t.Errorf("System.Now() location = %s, want UTC", loc)
	}
}

func TestFakeAdvance(t *testing.T) {
	start := time.Date(2026, 7, 20, 23, 59, 59, 0, time.UTC)
	c := clock.NewFake(start)

	if overdue(c, dueDate) {
		t.Fatal("precondition: should not be overdue at 23:59:59 on the due day")
	}

	c.Advance(time.Second)

	if !overdue(c, dueDate) {
		t.Error("after advancing one second past the due day, want overdue")
	}
}

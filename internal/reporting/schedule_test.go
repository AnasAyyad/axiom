package reporting

import (
	"testing"
	"time"
)

func TestScheduleNextUsesStrictUTCWindows(t *testing.T) {
	hour, weekday := 9, int(time.Monday)
	tests := []struct {
		name      string
		schedule  Schedule
		reference string
		want      string
	}{
		{"hourly before", Schedule{Frequency: Hourly, Minute: 15}, "2026-08-04T10:14:59Z", "2026-08-04T10:15:00Z"},
		{"hourly exact", Schedule{Frequency: Hourly, Minute: 15}, "2026-08-04T10:15:00Z", "2026-08-04T11:15:00Z"},
		{"daily rollover", Schedule{Frequency: Daily, Minute: 5, Hour: &hour}, "2026-08-04T09:05:00Z", "2026-08-05T09:05:00Z"},
		{"weekly rollover", Schedule{Frequency: Weekly, Minute: 30, Hour: &hour, Weekday: &weekday}, "2026-08-03T09:30:00Z", "2026-08-10T09:30:00Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference, _ := time.Parse(time.RFC3339, test.reference)
			want, _ := time.Parse(time.RFC3339, test.want)
			got, err := test.schedule.Next(reference)
			if err != nil || !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("Next() = %s, %v; want %s UTC", got, err, want)
			}
		})
	}
}

func TestScheduleRejectsAmbiguousFields(t *testing.T) {
	hour, weekday := 2, 1
	for _, schedule := range []Schedule{
		{Frequency: Hourly, Minute: 60},
		{Frequency: Hourly, Minute: 2, Hour: &hour},
		{Frequency: Daily, Minute: 2, Weekday: &weekday},
		{Frequency: Weekly, Minute: 2, Hour: &hour},
		{Frequency: "monthly", Minute: 2},
	} {
		if schedule.Validate() == nil {
			t.Fatalf("invalid schedule accepted: %#v", schedule)
		}
	}
}

package reporting

import (
	"fmt"
	"time"
)

// Frequency is the closed UTC report cadence.
type Frequency string

// Hourly, Daily, and Weekly are the only accepted UTC report frequencies.
const (
	// Hourly runs once per UTC hour at Minute.
	Hourly Frequency = "hourly"
	// Daily runs once per UTC day at Hour:Minute.
	Daily Frequency = "daily"
	// Weekly runs on Weekday at Hour:Minute UTC.
	Weekly Frequency = "weekly"
)

// Schedule is deliberately UTC-only so daylight-saving changes cannot shift a
// report or create a duplicate scheduling window.
type Schedule struct {
	Frequency Frequency
	Minute    int
	Hour      *int
	Weekday   *int
}

// Validate rejects ambiguous or incomplete cadence fields.
func (schedule Schedule) Validate() error {
	if schedule.Minute < 0 || schedule.Minute > 59 {
		return fmt.Errorf("report_schedule_minute_invalid")
	}
	switch schedule.Frequency {
	case Hourly:
		if schedule.Hour != nil || schedule.Weekday != nil {
			return fmt.Errorf("report_schedule_hourly_fields_invalid")
		}
	case Daily:
		if !validHour(schedule.Hour) || schedule.Weekday != nil {
			return fmt.Errorf("report_schedule_daily_fields_invalid")
		}
	case Weekly:
		if !validHour(schedule.Hour) || schedule.Weekday == nil || *schedule.Weekday < 0 || *schedule.Weekday > 6 {
			return fmt.Errorf("report_schedule_weekly_fields_invalid")
		}
	default:
		return fmt.Errorf("report_schedule_frequency_invalid")
	}
	return nil
}

func validHour(hour *int) bool { return hour != nil && *hour >= 0 && *hour <= 23 }

// Next returns the first exact scheduling instant strictly after reference.
func (schedule Schedule) Next(reference time.Time) (time.Time, error) {
	if err := schedule.Validate(); err != nil || reference.IsZero() {
		if err != nil {
			return time.Time{}, err
		}
		return time.Time{}, fmt.Errorf("report_schedule_reference_invalid")
	}
	reference = reference.UTC()
	switch schedule.Frequency {
	case Hourly:
		return nextHourly(reference, schedule.Minute), nil
	case Daily:
		return nextDaily(reference, *schedule.Hour, schedule.Minute), nil
	default:
		return nextWeekly(reference, *schedule.Weekday, *schedule.Hour, schedule.Minute), nil
	}
}

func nextHourly(reference time.Time, minute int) time.Time {
	candidate := time.Date(reference.Year(), reference.Month(), reference.Day(), reference.Hour(), minute, 0, 0, time.UTC)
	if !candidate.After(reference) {
		candidate = candidate.Add(time.Hour)
	}
	return candidate
}

func nextDaily(reference time.Time, hour, minute int) time.Time {
	candidate := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, time.UTC)
	if !candidate.After(reference) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}

func nextWeekly(reference time.Time, weekday, hour, minute int) time.Time {
	candidate := time.Date(reference.Year(), reference.Month(), reference.Day(), hour, minute, 0, 0, time.UTC)
	days := (weekday - int(candidate.Weekday()) + 7) % 7
	candidate = candidate.AddDate(0, 0, days)
	if !candidate.After(reference) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

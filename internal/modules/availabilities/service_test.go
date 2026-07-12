package availabilities

import (
	"testing"
	"time"
)

func TestConvertScheduleUTCToLocation_Paris_ShiftsDayAndTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	// Monday 23:30 UTC -> Tuesday 01:30 in Paris (CEST)
	schedule := AvailabilitySchedule{
		DayOfWeek: 1,
		StartTime: "23:30:00",
		EndTime:   "23:59:00",
	}

	ref := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	got := convertScheduleUTCToLocation(schedule, loc, ref)

	if got.DayOfWeek != 2 {
		t.Fatalf("expected day_of_week=2, got %d", got.DayOfWeek)
	}
	if got.StartTime != "01:30:00" {
		t.Fatalf("expected start_time=01:30:00, got %s", got.StartTime)
	}
	if got.EndTime != "01:59:00" {
		t.Fatalf("expected end_time=01:59:00, got %s", got.EndTime)
	}
}

func TestConvertAvailabilitiesSchedulesFromUTC_ConvertsAllSchedules(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	input := []Availability{
		{
			AvailabilityID: "a1",
			Schedules: []AvailabilitySchedule{
				{DayOfWeek: 5, StartTime: "10:00:00", EndTime: "12:00:00"},
				{DayOfWeek: 5, StartTime: "22:30:00", EndTime: "23:30:00"},
			},
		},
	}

	ref := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	got := convertAvailabilitiesSchedulesFromUTC(input, loc, ref)

	if len(got) != 1 || len(got[0].Schedules) != 2 {
		t.Fatalf("unexpected schedules length after conversion")
	}

	if got[0].Schedules[0].StartTime != "12:00:00" || got[0].Schedules[0].EndTime != "14:00:00" {
		t.Fatalf("expected +2h conversion on first schedule, got %s-%s", got[0].Schedules[0].StartTime, got[0].Schedules[0].EndTime)
	}

	if got[0].Schedules[1].DayOfWeek != 6 {
		t.Fatalf("expected second schedule day_of_week to move to 6, got %d", got[0].Schedules[1].DayOfWeek)
	}
	if got[0].Schedules[1].StartTime != "00:30:00" || got[0].Schedules[1].EndTime != "01:30:00" {
		t.Fatalf("expected midnight crossing conversion on second schedule, got %s-%s", got[0].Schedules[1].StartTime, got[0].Schedules[1].EndTime)
	}
}

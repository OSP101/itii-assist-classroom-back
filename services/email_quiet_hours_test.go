package services

import (
	"testing"
	"time"
)

func thaiAt(day, hour, minute, second int) time.Time {
	return time.Date(2026, time.September, day, hour, minute, second, 0, thaiTime)
}

func TestInstructorEmailWindowOpen(t *testing.T) {
	cases := []struct {
		at   time.Time
		want bool
	}{
		{thaiAt(26, 0, 30, 0), false},
		{thaiAt(26, 8, 0, 59), false},
		{thaiAt(26, 8, 1, 0), true},
		{thaiAt(26, 13, 0, 0), true},
		{thaiAt(26, 20, 0, 59), true},
		{thaiAt(26, 20, 1, 0), false},
		{thaiAt(26, 23, 59, 0), false},
		// 01:30 UTC is 08:30 in Thailand.
		{time.Date(2026, time.September, 26, 1, 30, 0, 0, time.UTC), true},
	}
	for _, c := range cases {
		if got := InstructorEmailWindowOpen(c.at); got != c.want {
			t.Errorf("InstructorEmailWindowOpen(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestNextInstructorEmailWindow(t *testing.T) {
	cases := []struct {
		at   time.Time
		want time.Time
	}{
		{thaiAt(26, 3, 0, 0), thaiAt(26, 8, 1, 0)},
		{thaiAt(26, 8, 0, 30), thaiAt(26, 8, 1, 0)},
		{thaiAt(26, 20, 1, 0), thaiAt(27, 8, 1, 0)},
		{thaiAt(30, 23, 45, 0), time.Date(2026, time.October, 1, 8, 1, 0, 0, thaiTime)},
	}
	for _, c := range cases {
		if got := nextInstructorEmailWindow(c.at); !got.Equal(c.want) {
			t.Errorf("nextInstructorEmailWindow(%s) = %s, want %s", c.at, got, c.want)
		}
	}
}

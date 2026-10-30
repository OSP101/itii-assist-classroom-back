package handlers

import (
	"itii-assist/models"
	"testing"
	"time"
)

func codes(ps []attendanceSettingsProblem) map[string]bool {
	m := map[string]bool{}
	for _, p := range ps {
		m[p.Code] = true
	}
	return m
}

func f64(v float64) *float64 { return &v }

func TestValidateNewAttendanceTimes(t *testing.T) {
	now := time.Date(2026, 10, 18, 6, 5, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 18, h, m, 0, 0, time.UTC) }
	cases := []struct {
		name             string
		start, late, end time.Time
		want             []string
	}{
		{"valid future", at(8, 0), at(8, 15), at(10, 0), nil},
		{"started but still open is allowed", at(5, 30), at(5, 45), at(8, 30), nil},
		{"end before start", at(9, 0), at(9, 0), at(8, 0), []string{"end_before_start", "late_out_of_range"}},
		{"shorter than five minutes", at(8, 0), at(8, 0), at(8, 3), []string{"too_short"}},
		{"late after end", at(8, 0), at(10, 30), at(10, 0), []string{"late_out_of_range"}},
		{"late before start", at(8, 0), at(7, 50), at(10, 0), []string{"late_out_of_range"}},
		{"already ended", at(2, 0), at(2, 15), at(5, 0), []string{"already_ended"}},
		{"too far ahead", at(8, 0).AddDate(1, 0, 0), at(8, 15).AddDate(1, 0, 0), at(10, 0).AddDate(1, 0, 0), []string{"too_far_ahead"}},
	}
	for _, tc := range cases {
		got := codes(validateNewAttendanceTimes(attendanceSessionSettings{Start: tc.start, Late: tc.late, End: tc.end}, now))
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for _, w := range tc.want {
			if !got[w] {
				t.Errorf("%s: missing %s in %v", tc.name, w, got)
			}
		}
	}
}

func TestValidateAttendanceSecurity(t *testing.T) {
	onCampus := [2]float64{16.474912, 102.823561} // College of Computing
	nongKhai := [2]float64{17.8049854, 102.7475011}
	offCampus := [2]float64{16.4322, 102.8250} // in town, south of campus
	cases := []struct {
		name string
		in   attendanceSessionSettings
		want []string
	}{
		{"network and pin on campus", attendanceSessionSettings{SessionType: "lab", RequireCampusNetwork: true, CheckLocation: true, LocationLat: f64(onCampus[0]), LocationLng: f64(onCampus[1]), RadiusMeters: 50}, nil},
		{"network and pin on Nong Khai campus", attendanceSessionSettings{SessionType: "lab", RequireCampusNetwork: true, CheckLocation: true, LocationLat: f64(nongKhai[0]), LocationLng: f64(nongKhai[1]), RadiusMeters: 50}, nil},
		{"network but pin off campus", attendanceSessionSettings{SessionType: "lab", RequireCampusNetwork: true, CheckLocation: true, LocationLat: f64(offCampus[0]), LocationLng: f64(offCampus[1]), RadiusMeters: 50}, []string{"campus_network_pin_off_campus"}},
		{"field trip without network is fine", attendanceSessionSettings{SessionType: "lab", CheckLocation: true, LocationLat: f64(offCampus[0]), LocationLng: f64(offCampus[1]), RadiusMeters: 100}, nil},
		{"gps without pin", attendanceSessionSettings{SessionType: "lecture", CheckLocation: true, RadiusMeters: 100}, []string{"gps_without_pin"}},
		{"radius too big", attendanceSessionSettings{SessionType: "lecture", CheckLocation: true, LocationLat: f64(onCampus[0]), LocationLng: f64(onCampus[1]), RadiusMeters: 5000}, []string{"radius_out_of_range"}},
		{"online with network", attendanceSessionSettings{SessionType: "online", RequireCampusNetwork: true}, []string{"online_campus_network"}},
		{"online with gps", attendanceSessionSettings{SessionType: "online", CheckLocation: true, LocationLat: f64(onCampus[0]), LocationLng: f64(onCampus[1]), RadiusMeters: 50}, []string{"online_gps"}},
		{"online open", attendanceSessionSettings{SessionType: "online"}, nil},
	}
	for _, tc := range cases {
		got := codes(validateAttendanceSecurity(tc.in))
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for _, w := range tc.want {
			if !got[w] {
				t.Errorf("%s: missing %s in %v", tc.name, w, got)
			}
		}
	}
}

func TestSameAttendanceSecurity(t *testing.T) {
	lat, lng := 13.7563, 100.5018 // off campus, saved before the rules existed
	legacy := models.AttendanceSession{SessionType: "lecture", CheckLocation: true, LocationLat: &lat, LocationLng: &lng, RadiusMeters: 50}
	before := attendanceSecurityOf(legacy)

	// The edit form resends the same values as fresh pointers: not a change.
	lat2, lng2 := lat, lng
	resent := legacy
	resent.LocationLat, resent.LocationLng = &lat2, &lng2
	yes := true
	resent.RequireMobile, resent.RequireCampusNetwork = &yes, &yes
	if !sameAttendanceSecurity(before, attendanceSecurityOf(resent)) {
		t.Fatal("resending unchanged settings must not count as a change")
	}

	moved := resent
	other := 16.47
	moved.LocationLat = &other
	if sameAttendanceSecurity(before, attendanceSecurityOf(moved)) {
		t.Fatal("moving the pin must count as a change")
	}
	no := false
	relaxed := resent
	relaxed.RequireMobile = &no
	if sameAttendanceSecurity(before, attendanceSecurityOf(relaxed)) {
		t.Fatal("turning off the device check must count as a change")
	}
}

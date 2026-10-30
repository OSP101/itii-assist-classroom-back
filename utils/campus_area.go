package utils

// CampusArea is a latitude/longitude box around one university campus.
type CampusArea struct {
	Name           string
	MinLat, MaxLat float64
	MinLng, MaxLng float64
}

// KKUCampusAreas are the bounding boxes of Khon Kaen University's campuses,
// taken from OpenStreetMap (relation 812288 for the main campus, way
// 545371029 for Nong Khai). A bounding box is larger than the campus itself,
// so a point inside a real campus is never reported as outside. The frontend
// keeps the same list in lib/attendance/campus-areas.ts; change both together.
var KKUCampusAreas = []CampusArea{
	{Name: "มหาวิทยาลัยขอนแก่น", MinLat: 16.4413317, MaxLat: 16.4821091, MinLng: 102.8023410, MaxLng: 102.8323324},
	{Name: "มหาวิทยาลัยขอนแก่น วิทยาเขตหนองคาย", MinLat: 17.7966963, MaxLat: 17.8122636, MinLng: 102.7427380, MaxLng: 102.7518092},
}

// campusMarginDeg widens every box by about 220 m so a pin on a building at
// the campus edge, or a GPS fix a little off, still counts as on campus.
const campusMarginDeg = 0.002

// InsideKKUCampus reports whether a point lies within any KKU campus area.
func InsideKKUCampus(lat, lng float64) bool {
	for _, a := range KKUCampusAreas {
		if lat >= a.MinLat-campusMarginDeg && lat <= a.MaxLat+campusMarginDeg &&
			lng >= a.MinLng-campusMarginDeg && lng <= a.MaxLng+campusMarginDeg {
			return true
		}
	}
	return false
}

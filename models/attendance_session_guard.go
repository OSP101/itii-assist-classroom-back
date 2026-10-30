package models

// MobileRequired reports whether check-in for this session is limited to
// phones and tablets. A missing value (rows or cached copies from before the
// column existed) means required, which is how every session behaved then.
func (s AttendanceSession) MobileRequired() bool {
	return s.RequireMobile == nil || *s.RequireMobile
}

// CampusNetworkRequired reports whether check-in for this session must come
// from the university network. A missing value means required.
func (s AttendanceSession) CampusNetworkRequired() bool {
	return s.RequireCampusNetwork == nil || *s.RequireCampusNetwork
}

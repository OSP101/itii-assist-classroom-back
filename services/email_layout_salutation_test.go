package services

import "testing"

func TestEmailSalutation(t *testing.T) {
	cases := []struct {
		name, role, want string
	}{
		{"สมชาย ใจดี", "instructor", "อาจารย์สมชาย ใจดี"},
		{"ผศ.ดร.สมชาย ใจดี", "instructor", "ผศ.ดร.สมชาย ใจดี"},
		{"ดร.สมหญิง รักเรียน", "instructor", "ดร.สมหญิง รักเรียน"},
		{"อ.สมศักดิ์ มานะ", "instructor", "อ.สมศักดิ์ มานะ"},
		{"สมศรี ขยัน", "ta", "คุณสมศรี ขยัน"},
		{"สมพร ดูแลระบบ", "admin", "อาจารย์สมพร ดูแลระบบ"},
		{"นางสาวทดสอบ ระบบ", "student", "นางสาวทดสอบ ระบบ"},
		{"ไม่ระบุบทบาท", "", "คุณไม่ระบุบทบาท"},
	}
	for _, c := range cases {
		if got := emailSalutation(c.name, c.role); got != c.want {
			t.Errorf("emailSalutation(%q, %q) = %q, want %q", c.name, c.role, got, c.want)
		}
	}
	if got := emailGreetingPlain("สมชาย", "instructor"); got != "สวัสดี อาจารย์สมชาย," {
		t.Errorf("emailGreetingPlain = %q", got)
	}
}

package handlers

import (
	"encoding/json"
	"itii-assist/middlewares"
	"itii-assist/realtime"
	"itii-assist/repositories"
	"itii-assist/utils"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Room names are constructed here from the caller's identity/permissions;
// clients cannot turn a ticket for their own booking into a staff ticket.
func GetRealtimeRoomTicketHandler(c fiber.Ctx) error {
	var input struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := c.Bind().JSON(&input); err != nil {
		return fiber.ErrBadRequest
	}
	var id string
	if json.Unmarshal(input.Data, &id) != nil {
		var n uint
		if json.Unmarshal(input.Data, &n) == nil {
			id = strconv.FormatUint(uint64(n), 10)
		}
	}
	uid, staff := middlewares.GetUserID(c)
	sid, student := middlewares.GetStudentID(c)
	role, _ := middlewares.GetUserRole(c)
	room := ""
	switch input.Event {
	case "join-user", "join-user-courses":
		if !staff || id != strconv.FormatUint(uint64(uid), 10) {
			return fiber.ErrForbidden
		}
		if input.Event == "join-user" {
			room = "user-" + id
		} else {
			room = "user-courses-" + id
		}
	case "join-global-updates":
		if !staff {
			return fiber.ErrForbidden
		}
		room = "global-updates"
	case "join-classroom":
		if !staff || (role != "admin" && role != "instructor" && role != "ta") || id == "" {
			return fiber.ErrForbidden
		}
		room = "classroom-" + id
	case "join-queue-staff":
		courseID, err := repositories.GetCourseIDByQueueSessionID(id)
		if err != nil || !queueStaffAllowed(c, courseID, repositories.PermissionViewQueue) {
			return fiber.ErrForbidden
		}
		room = "queue-staff-" + id
	case "join-booking":
		bid, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return fiber.ErrBadRequest
		}
		booking, err := repositories.GetBookingByID(uint(bid))
		if err != nil || !queueBookingAllowed(c, booking, repositories.PermissionViewQueue) {
			return fiber.ErrForbidden
		}
		room = "booking-" + id
	default:
		return fiber.ErrForbidden
	}
	claims := utils.TokenClaims{UserID: uid, Role: role, Kind: "u"}
	if student {
		claims.UserID, claims.Kind = sid, "s"
	}
	claims.JTI, _ = c.Locals("jti").(string)
	claims.ExpiresAt, _ = c.Locals("access_expires_at").(time.Time)
	ticket, expiry, err := realtime.IssuePrincipalSocketTicket(room, claims, time.Minute)
	if err != nil {
		return fiber.ErrServiceUnavailable
	}
	return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"ticket": ticket, "expires_at": expiry}})
}

func issueCallerSocketTicket(c fiber.Ctx, room string, ttl time.Duration) (string, time.Time, error) {
	uid, _ := middlewares.GetUserID(c)
	role, _ := middlewares.GetUserRole(c)
	jti, _ := c.Locals("jti").(string)
	expiry, _ := c.Locals("access_expires_at").(time.Time)
	return realtime.IssuePrincipalSocketTicket(room, utils.TokenClaims{UserID: uid, Role: role, Kind: "u", JTI: jti, ExpiresAt: expiry}, ttl)
}

func SocketRoomPermissionAllowed(room string, principal utils.TokenClaims) bool {
	courseID := ""
	switch {
	case strings.HasPrefix(room, "instructor-"):
		id, err := strconv.ParseUint(strings.TrimPrefix(room, "instructor-"), 10, 64)
		if err != nil {
			return false
		}
		courseID, err = repositories.GetCourseIDByAttendanceSessionID(uint(id))
		if err != nil {
			return false
		}
	case strings.HasPrefix(room, "queue-staff-"):
		var err error
		courseID, err = repositories.GetCourseIDByQueueSessionID(strings.TrimPrefix(room, "queue-staff-"))
		if err != nil {
			return false
		}
	default:
		return true
	}
	if principal.Kind != "u" {
		return false
	}
	if principal.Role == "admin" {
		return true
	}
	_, allowed, err := repositories.GetCourseAccessState(courseID, principal.UserID, "instructor", "ta")
	if err != nil || !allowed {
		return false
	}
	permission := repositories.PermissionViewQueue
	if strings.HasPrefix(room, "instructor-") {
		permission = repositories.PermissionViewAttendance
	}
	allowed, err = repositories.HasCoursePermission(courseID, principal.UserID, principal.Role, permission)
	return err == nil && allowed
}

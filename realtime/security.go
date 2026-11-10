package realtime

import (
	"encoding/json"
	"itii-assist/config"
	"itii-assist/models"
	"itii-assist/utils"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var socketConnections = struct {
	sync.Mutex
	active int
	byIP   map[string]int
}{byIP: map[string]int{}}

func socketLimit(name string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func reserveSocketConnection(ip string) (func(), bool) {
	socketConnections.Lock()
	if socketConnections.active >= socketLimit("WEBSOCKET_MAX_CONNECTIONS", 8192) || socketConnections.byIP[ip] >= socketLimit("WEBSOCKET_MAX_CONNECTIONS_PER_IP", 4096) {
		socketConnections.Unlock()
		return nil, false
	}
	socketConnections.active++
	socketConnections.byIP[ip]++
	socketConnections.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			socketConnections.Lock()
			defer socketConnections.Unlock()
			socketConnections.active--
			socketConnections.byIP[ip]--
			if socketConnections.byIP[ip] == 0 {
				delete(socketConnections.byIP, ip)
			}
		})
	}, true
}

type principalTicket struct {
	Room      string            `json:"room"`
	Principal utils.TokenClaims `json:"principal"`
}

type displayGrantTicket struct {
	Room    string `json:"room"`
	GrantID string `json:"grant_id"`
}

func IssueGrantSocketTicket(room, grantID string, ttl time.Duration) (string, time.Time, error) {
	raw, err := json.Marshal(displayGrantTicket{Room: room, GrantID: grantID})
	if err != nil {
		return "", time.Time{}, err
	}
	return IssueSocketTicket("display-grant:"+string(raw), ttl)
}

func activeDisplayGrant(id string) bool {
	if config.DB == nil || id == "" {
		return false
	}
	var count int64
	return config.DB.Model(&models.AttendanceDisplayGrant{}).Where("id = ? AND status = ? AND revoked_at IS NULL AND expires_at > ?", id, "active", time.Now()).Count(&count).Error == nil && count == 1
}

func IssuePrincipalSocketTicket(room string, principal utils.TokenClaims, ttl time.Duration) (string, time.Time, error) {
	raw, err := json.Marshal(principalTicket{Room: room, Principal: principal})
	if err != nil {
		return "", time.Time{}, err
	}
	return IssueSocketTicket("principal:"+string(raw), ttl)
}

func decodePrincipalTicket(value string) (string, *utils.TokenClaims, bool) {
	if strings.HasPrefix(value, "display-grant:") {
		var ticket displayGrantTicket
		if json.Unmarshal([]byte(strings.TrimPrefix(value, "display-grant:")), &ticket) != nil || !strings.HasPrefix(ticket.Room, "display-attendance-") || !activeDisplayGrant(ticket.GrantID) {
			return "", nil, false
		}
		return ticket.Room, nil, true
	}
	if !strings.HasPrefix(value, "principal:") {
		return value, nil, value != ""
	}
	var ticket principalTicket
	if json.Unmarshal([]byte(strings.TrimPrefix(value, "principal:")), &ticket) != nil || ticket.Room == "" {
		return "", nil, false
	}
	if !ticket.Principal.ExpiresAt.IsZero() && !ticket.Principal.ExpiresAt.After(time.Now()) {
		return "", nil, false
	}
	if config.ValidateSessionPrincipal(&ticket.Principal) != nil {
		return "", nil, false
	}
	return ticket.Room, &ticket.Principal, true
}

func socketOriginAllowed(origin, host string) bool {
	if origin == "" {
		return true
	} // Native clients have no browser Origin.
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	for _, allowed := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return strings.EqualFold(u.Host, host)
}

// Public rooms are invalidation feeds. Full records belong only in the
// authorized worker/booking rooms and permission-checked HTTP responses.
func publicQueueEvent(data interface{}) interface{} {
	raw, _ := json.Marshal(data)
	var value map[string]interface{}
	_ = json.Unmarshal(raw, &value)
	out := map[string]interface{}{"timestamp": nowMillis()}
	for _, key := range []string{"status", "session_id", "is_cutoff_enabled", "cutoff_at", "reason", "scope"} {
		if v, ok := value[key]; ok {
			out[key] = v
		}
	}
	return out
}

func publicUpdateData(data interface{}) interface{} {
	raw, _ := json.Marshal(data)
	var value map[string]interface{}
	_ = json.Unmarshal(raw, &value)
	out := map[string]interface{}{}
	for _, key := range []string{"course_id", "courseId", "actor_id", "actorId", "session_id"} {
		if v, ok := value[key]; ok {
			switch v.(type) {
			case string, float64, bool:
				out[key] = v
			}
		}
	}
	return out
}

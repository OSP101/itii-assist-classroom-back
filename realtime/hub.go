package realtime

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

type incomingMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type outgoingMessage struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data,omitempty"`
}

type client struct {
	id    string
	hub   *hub
	conn  *websocket.Conn
	send  chan outgoingMessage
	rooms map[string]struct{}
}

type hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	rooms   map[string]map[*client]struct{}
}

var defaultHub = &hub{
	clients: make(map[*client]struct{}),
	rooms:   make(map[string]map[*client]struct{}),
}

var upgrader = websocket.FastHTTPUpgrader{
	HandshakeTimeout:  10 * time.Second,
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	EnableCompression: true,
	CheckOrigin: func(ctx *fasthttp.RequestCtx) bool {
		return true
	},
}

func Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !c.IsWebSocket() {
			return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{"success": false, "message": "WebSocket upgrade required"})
		}

		return upgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
			client := &client{
				id:    fmt.Sprintf("go-%d", time.Now().UnixNano()),
				hub:   defaultHub,
				conn:  conn,
				send:  make(chan outgoingMessage, 64),
				rooms: make(map[string]struct{}),
			}

			defaultHub.register(client)
			client.send <- outgoingMessage{Event: "socket-ready", Data: fiber.Map{"id": client.id}}

			go client.writePump()
			client.readPump()
		})
	}
}

func (h *hub) register(client *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = struct{}{}
	log.Printf("realtime: client connected %s", client.id)
}

func (h *hub) unregister(client *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; !ok {
		return
	}
	delete(h.clients, client)
	for room := range client.rooms {
		h.leaveLocked(client, room)
	}
	close(client.send)
	log.Printf("realtime: client disconnected %s", client.id)
}

func (h *hub) join(member *client, room string) {
	room = strings.TrimSpace(room)
	if room == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[*client]struct{})
	}
	h.rooms[room][member] = struct{}{}
	member.rooms[room] = struct{}{}
}

func (h *hub) leave(member *client, room string) {
	room = strings.TrimSpace(room)
	if room == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.leaveLocked(member, room)
}

func (h *hub) leaveLocked(client *client, room string) {
	delete(client.rooms, room)
	if clients, ok := h.rooms[room]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.rooms, room)
		}
	}
}

func (h *hub) broadcast(room string, event string, data interface{}, except *client) {
	room = strings.TrimSpace(room)
	if room == "" || event == "" {
		return
	}

	h.mu.RLock()
	targets := make([]*client, 0, len(h.rooms[room]))
	for client := range h.rooms[room] {
		if client != except {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	message := outgoingMessage{Event: event, Data: data}
	for _, target := range targets {
		select {
		case target.send <- message:
		default:
			go h.unregister(target)
		}
	}
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()

	for {
		var message incomingMessage
		if err := c.conn.ReadJSON(&message); err != nil {
			return
		}
		c.handleMessage(message)
	}
}

func (c *client) writePump() {
	for message := range c.send {
		if err := c.conn.WriteJSON(message); err != nil {
			return
		}
	}
}

func (c *client) handleMessage(message incomingMessage) {
	switch message.Event {
	case "join-attendance":
		c.hub.join(c, "attendance-"+rawString(message.Data))
	case "leave-attendance":
		c.hub.leave(c, "attendance-"+rawString(message.Data))
	case "join-instructor":
		c.hub.join(c, "instructor-"+rawString(message.Data))
	case "leave-instructor":
		c.hub.leave(c, "instructor-"+rawString(message.Data))
	case "join-user-courses":
		c.hub.join(c, "user-courses-"+rawString(message.Data))
		c.hub.join(c, "global-courses")
	case "leave-user-courses":
		c.hub.leave(c, "user-courses-"+rawString(message.Data))
		c.hub.leave(c, "global-courses")
	case "course-change":
		payload := rawMap(message.Data)
		payload["timestamp"] = nowMillis()
		c.hub.broadcast("global-courses", "course-updated", payload, c)
	case "join-classroom":
		c.hub.join(c, "classroom-"+rawString(message.Data))
	case "leave-classroom":
		c.hub.leave(c, "classroom-"+rawString(message.Data))
	case "classroom-change":
		payload := rawMap(message.Data)
		room := "classroom-" + fmt.Sprint(payload["classroomId"])
		c.hub.broadcast(room, "classroom-updated", fiber.Map{"type": payload["type"], "payload": payload["payload"], "timestamp": nowMillis()}, c)
	case "join-global-updates":
		c.hub.join(c, "global-updates")
	case "leave-global-updates":
		c.hub.leave(c, "global-updates")
	case "join-queue":
		c.hub.join(c, "queue-"+rawString(message.Data))
	case "leave-queue":
		c.hub.leave(c, "queue-"+rawString(message.Data))
	case "join-worker":
		c.hub.join(c, "worker-"+rawString(message.Data))
	case "leave-worker":
		c.hub.leave(c, "worker-"+rawString(message.Data))
	case "join-booking":
		c.hub.join(c, "booking-"+rawString(message.Data))
	case "leave-booking":
		c.hub.leave(c, "booking-"+rawString(message.Data))
	case "data-change":
		payload := rawMap(message.Data)
		resource, _ := payload["resource"].(string)
		update := fiber.Map{"resource": resource, "action": payload["action"], "id": payload["id"], "data": payload["data"], "timestamp": nowMillis()}
		c.hub.broadcast("global-updates", "data-updated", update, c)
		if resource == "course" {
			c.hub.broadcast("global-courses", "course-updated", fiber.Map{"action": payload["action"], "courseId": payload["id"], "timestamp": nowMillis()}, c)
		}
	}
}

func EmitToRoom(room string, event string, data interface{}) {
	defaultHub.broadcast(room, event, data, nil)
}

func EmitToAttendance(sessionID interface{}, event string, data interface{}) {
	EmitToRoom("attendance-"+fmt.Sprint(sessionID), event, data)
	EmitToRoom("instructor-"+fmt.Sprint(sessionID), event, data)
}

func EmitToInstructor(sessionID interface{}, event string, data interface{}) {
	EmitToRoom("instructor-"+fmt.Sprint(sessionID), event, data)
}

func EmitToQueue(sessionID interface{}, event string, data interface{}) {
	EmitToRoom("queue-"+fmt.Sprint(sessionID), event, data)
}

func EmitToBooking(bookingID interface{}, event string, data interface{}) {
	EmitToRoom("booking-"+fmt.Sprint(bookingID), event, data)
}

func EmitToWorker(userID interface{}, event string, data interface{}) {
	EmitToRoom("worker-"+fmt.Sprint(userID), event, data)
}

func EmitCourseUpdate(action string, courseID interface{}, userID interface{}) {
	EmitToRoom("global-courses", "course-updated", fiber.Map{"action": action, "courseId": courseID, "userId": userID, "timestamp": nowMillis()})
}

func EmitDataUpdate(resource string, action string, id interface{}, data interface{}) {
	EmitToRoom("global-updates", "data-updated", fiber.Map{"resource": resource, "action": action, "id": id, "data": data, "timestamp": nowMillis()})
}

func rawString(data json.RawMessage) string {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func rawMap(data json.RawMessage) map[string]interface{} {
	value := map[string]interface{}{}
	_ = json.Unmarshal(data, &value)
	return value
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

package realtime

import (
	"crypto/rand"
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

// Keepalive budget for the WebSocket connections. Without these a client that
// vanishes without a TCP FIN (phone screen off, WiFi→4G handover, the KKU proxy
// dropping an idle tunnel) stays registered in the hub forever: ReadJSON blocks
// until the OS TCP timeout, which may never arrive, and writePump blocks inside
// WriteJSON on a half-open socket. Those ghost clients accumulate every class
// session and make every broadcast walk a longer and longer room map.
//
// pingPeriod must stay comfortably below pongWait so a single dropped ping does
// not kill a healthy connection. Browsers answer protocol-level ping frames in
// the WebSocket layer itself, so no client-side change is needed.
const (
	pongWait   = 60 * time.Second
	pingPeriod = 25 * time.Second
	writeWait  = 10 * time.Second
)

type client struct {
	id    string
	hub   *hub
	conn  *websocket.Conn
	send  chan outgoingMessage
	rooms map[string]struct{}

	// done is closed exactly once when the client is unregistered. The send
	// channel is deliberately never closed: broadcast() hands messages over
	// after releasing the hub lock, so closing it would let a concurrent
	// unregister turn a broadcast into a send-on-closed-channel panic — and a
	// panic on a WebSocket goroutine takes the whole process down.
	done      chan struct{}
	closeOnce sync.Once
}

func (c *client) markDone() {
	c.closeOnce.Do(func() { close(c.done) })
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

type socketTicket struct {
	room      string
	expiresAt time.Time
}

var displaySocketTickets = struct {
	mu    sync.Mutex
	items map[string]socketTicket
}{
	items: make(map[string]socketTicket),
}

var upgrader = websocket.FastHTTPUpgrader{
	HandshakeTimeout: 10 * time.Second,
	ReadBufferSize:   1024,
	WriteBufferSize:  1024,
	// Compression is off on purpose: the payloads here are small JSON events,
	// so per-message deflate buys almost nothing while costing a sizeable
	// flate buffer per open connection — real memory pressure once a few
	// hundred students are connected at once.
	EnableCompression: false,
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
				done:  make(chan struct{}),
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
	client.markDone()
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
		// Checked in its own select first: folding this into the send below
		// would leave both cases ready at once, and select picks at random, so
		// a disconnected client would still get messages roughly half the time.
		select {
		case <-target.done:
			// Already unregistered by a concurrent disconnect — drop it.
			continue
		default:
		}

		select {
		case target.send <- message:
		default:
			// Buffer full: the client is not draining, so drop it rather than
			// letting a stalled reader block every other subscriber.
			go h.unregister(target)
		}
	}
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()

	// Every frame the peer sends — including the pong answering our ping —
	// pushes the deadline out. A peer that goes silent for pongWait is treated
	// as gone, which is the only thing that reliably reaps half-open sockets.
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		var message incomingMessage
		if err := c.conn.ReadJSON(&message); err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.handleMessage(message)
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		// Closing the connection here is what unblocks readPump when the write
		// side is the one that failed; otherwise readPump would sit in ReadJSON
		// on a dead socket and the client would never be unregistered.
		_ = c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		case message := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteJSON(message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
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
		payload["timestamp"] = nowMillis()
		c.hub.broadcast(room, "classroom-updated", payload, c)
	case "join-global-updates":
		c.hub.join(c, "global-updates")
	case "leave-global-updates":
		c.hub.leave(c, "global-updates")
	case "join-queue":
		c.hub.join(c, "queue-"+rawString(message.Data))
	case "leave-queue":
		c.hub.leave(c, "queue-"+rawString(message.Data))
	case "join-queue-group":
		c.hub.join(c, "queue-group-"+rawString(message.Data))
	case "leave-queue-group":
		c.hub.leave(c, "queue-group-"+rawString(message.Data))
	case "join-worker":
		c.hub.join(c, "worker-"+rawString(message.Data))
	case "leave-worker":
		c.hub.leave(c, "worker-"+rawString(message.Data))
	case "join-booking":
		c.hub.join(c, "booking-"+rawString(message.Data))
	case "leave-booking":
		c.hub.leave(c, "booking-"+rawString(message.Data))
	case "join-user":
		c.hub.join(c, "user-"+rawString(message.Data))
	case "leave-user":
		c.hub.leave(c, "user-"+rawString(message.Data))
	case "join-display":
		payload := rawMap(message.Data)
		ticket := strings.TrimSpace(fmt.Sprint(payload["ticket"]))
		if room, ok := validateDisplaySocketTicket(ticket); ok {
			c.hub.join(c, room)
		}
	case "leave-display":
		payload := rawMap(message.Data)
		room := strings.TrimSpace(fmt.Sprint(payload["room"]))
		if room != "" {
			c.hub.leave(c, room)
		}
	case "data-change":
		payload := rawMap(message.Data)
		resource, _ := payload["resource"].(string)
		update := fiber.Map{"resource": resource, "action": payload["action"], "id": payload["id"], "data": payload["data"], "timestamp": nowMillis()}
		c.hub.broadcast("global-updates", "data-updated", update, c)
		if resource == "course" {
			actorID := interface{}(nil)
			if dataPayload, ok := payload["data"].(map[string]interface{}); ok {
				if value, exists := dataPayload["actor_id"]; exists {
					actorID = value
				} else if value, exists := dataPayload["actorId"]; exists {
					actorID = value
				}
			}
			c.hub.broadcast("global-courses", "course-updated", fiber.Map{"action": payload["action"], "courseId": payload["id"], "actor_id": actorID, "timestamp": nowMillis()}, c)
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

func EmitToAttendanceDisplay(sessionID interface{}, event string, data interface{}) {
	EmitToRoom("display-attendance-"+fmt.Sprint(sessionID), event, data)
}

func EmitToQueue(sessionID interface{}, event string, data interface{}) {
	EmitToRoom("queue-"+fmt.Sprint(sessionID), event, data)
}

// EmitToQueueGroup broadcasts an event to every client subscribed to the
// concurrent group's shared room. Used so projector/worker clients receive
// booking/state events from partner sessions in a shared-room setup.
func EmitToQueueGroup(groupID interface{}, event string, data interface{}) {
	group := fmt.Sprint(groupID)
	if group == "" || group == "<nil>" {
		return
	}
	EmitToRoom("queue-group-"+group, event, data)
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

func EmitToUser(userID interface{}, event string, data interface{}) {
	EmitToRoom(fmt.Sprintf("user-%v", userID), event, data)
}

func IssueDisplaySocketTicket(room string, ttl time.Duration) (string, time.Time, error) {
	room = strings.TrimSpace(room)
	if room == "" {
		return "", time.Time{}, fmt.Errorf("room required")
	}
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", time.Time{}, err
	}
	ticket := fmt.Sprintf("%x", buffer)
	expiresAt := time.Now().Add(ttl)
	displaySocketTickets.mu.Lock()
	defer displaySocketTickets.mu.Unlock()
	cleanupExpiredDisplayTicketsLocked()
	displaySocketTickets.items[ticket] = socketTicket{room: room, expiresAt: expiresAt}
	return ticket, expiresAt, nil
}

func validateDisplaySocketTicket(ticket string) (string, bool) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return "", false
	}
	displaySocketTickets.mu.Lock()
	defer displaySocketTickets.mu.Unlock()
	cleanupExpiredDisplayTicketsLocked()
	item, ok := displaySocketTickets.items[ticket]
	if !ok || time.Now().After(item.expiresAt) {
		if ok {
			delete(displaySocketTickets.items, ticket)
		}
		return "", false
	}
	return item.room, true
}

func cleanupExpiredDisplayTicketsLocked() {
	now := time.Now()
	for ticket, item := range displaySocketTickets.items {
		if now.After(item.expiresAt) {
			delete(displaySocketTickets.items, ticket)
		}
	}
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

// Stats reports how many WebSocket clients and rooms the hub currently holds.
// Exposed as Prometheus gauges so a ghost-connection leak shows up as a count
// that only ever climbs, instead of only being noticed as "the site got slow
// again a few days after the last restart".
func Stats() (clients int, rooms int) {
	defaultHub.mu.RLock()
	defer defaultHub.mu.RUnlock()
	return len(defaultHub.clients), len(defaultHub.rooms)
}

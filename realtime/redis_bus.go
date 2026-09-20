package realtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"itii-assist/config"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// realtimeEventsChannel is the single Redis Pub/Sub channel every backend
// replica publishes to and subscribes on (plan.md ระยะ 4.1). It is a
// TRANSPORT ONLY — there is no durability, no replay, no guarantee a
// subscriber that was briefly disconnected receives what it missed. The
// database is the source of truth for every piece of state broadcast here
// (attendance_sessions for the PIN, attendance_records for check-ins, ...);
// this bus exists purely to fan a same-process broadcast out to every OTHER
// replica's local clients too. See resyncLocalRooms for how a subscriber
// that reconnects after missing messages recovers, instead of silently
// serving stale state forever.
const realtimeEventsChannel = "realtime:events"

// busOrigin identifies this process among every replica sharing the bus, so
// handleBusMessage can recognise and skip an event this same process
// already delivered directly via EmitToRoom's local broadcast() call —
// without it, every event this instance emits would loop back through
// Redis and be broadcast to its own local clients a second time.
var busOrigin = newBusOrigin()

func newBusOrigin() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf) // crypto/rand.Read never errors on a live OS
	return hex.EncodeToString(buf)
}

type busMessage struct {
	Origin string      `json:"origin"`
	Room   string      `json:"room"`
	Event  string      `json:"event"`
	Data   interface{} `json:"data,omitempty"`
}

// publishRealtimeEvent fans a room broadcast out to every other replica.
// Fire-and-forget and non-blocking: EmitToRoom already delivered this
// event to this process's own local clients before calling here, so a
// slow or failed publish only means OTHER replicas' clients miss one
// event — never something this instance's own callers need to wait on or
// handle an error for.
func publishRealtimeEvent(room string, event string, data interface{}) {
	if config.Redis == nil {
		return
	}
	payload, err := json.Marshal(busMessage{Origin: busOrigin, Room: room, Event: event, Data: data})
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := config.Redis.Publish(ctx, realtimeEventsChannel, payload).Err(); err != nil {
			log.Printf("realtime bus: publish failed room=%s event=%s: %v", room, event, err)
		}
	}()
}

// StartRedisBus subscribes this process to the shared realtime channel and
// runs until the process exits. Call it once from cmd/api/main.go. Safe to
// call with config.Redis == nil (single-instance / no-Redis deployments):
// EmitToRoom's local broadcast already delivers every event to this
// process's own clients regardless, so a backend that never runs this
// simply behaves exactly as it did before ระยะ 4 — every WebSocket client
// must then be connected to THIS SAME process to receive anything, which is
// fine for one replica and is the whole reason multi-replica needs this.
func StartRedisBus() {
	if config.Redis == nil {
		log.Println("realtime bus: Redis not configured — running single-instance-only (no cross-replica fan-out)")
		return
	}
	go runBusSubscribeLoop()
}

// runBusSubscribeLoop owns its own (re)subscribe cycle rather than using
// PubSub.Channel()'s built-in auto-reconnect, specifically so it can tell a
// genuine reconnect (this replica was disconnected from the bus and may
// have missed events) apart from the first subscribe — go-redis's Channel()
// retries silently under the hood and exposes no such signal. Every
// reconnect triggers resyncLocalRooms() (plan.md ระยะ 4.1: "เมื่อ subscriber
// reconnect ให้ hub ส่ง event resync").
func runBusSubscribeLoop() {
	first := true
	for {
		ctx := context.Background()
		pubsub := config.Redis.Subscribe(ctx, realtimeEventsChannel)

		if _, err := pubsub.Receive(ctx); err != nil {
			log.Printf("realtime bus: subscribe failed, retrying: %v", err)
			_ = pubsub.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		if !first {
			log.Println("realtime bus: resubscribed after a disconnect — resyncing local clients")
			resyncLocalRooms()
		}
		first = false

		for {
			msg, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				log.Printf("realtime bus: subscription lost: %v", err)
				break
			}
			handleBusMessage(msg)
		}

		_ = pubsub.Close()
		time.Sleep(1 * time.Second)
	}
}

func handleBusMessage(msg *redis.Message) {
	var decoded busMessage
	if err := json.Unmarshal([]byte(msg.Payload), &decoded); err != nil {
		return
	}
	if decoded.Origin == busOrigin {
		// This process's own event, already delivered locally by the
		// direct broadcast() call inside EmitToRoom.
		return
	}
	defaultHub.broadcast(decoded.Room, decoded.Event, decoded.Data, nil)
}

// resyncLocalRooms tells every client currently in ANY room on this replica
// to resynchronize itself, by broadcasting a bare "resync" event local-only
// (deliberately NOT re-published to the bus — every other replica runs this
// same recovery independently on its own reconnect, and republishing would
// just echo the same event back and forth). The frontend treats "resync"
// exactly like a fresh "connect": re-fetch current state via its normal
// REST call (e.g. the check-in page's refreshPinState(), the instructor
// live view's fetchData()) rather than trusting that no events were missed
// while this replica was disconnected from the bus.
func resyncLocalRooms() {
	defaultHub.mu.RLock()
	rooms := make([]string, 0, len(defaultHub.rooms))
	for room := range defaultHub.rooms {
		rooms = append(rooms, room)
	}
	defaultHub.mu.RUnlock()

	for _, room := range rooms {
		defaultHub.broadcast(room, "resync", nil, nil)
	}
}

# Queue Attendance Block Log Queries

This runbook helps investigate cases where queue booking is blocked because attendance status has not propagated yet.

## Log Events

The backend now emits throttled events:

- `queue_validate attendance_blocked ...`
- `queue_create attendance_blocked ...`

Fields:

- `session`
- `linked_attendance_session_id`
- `student_code`
- `desk_number`
- `booking_type`
- `cooldown_seconds`

## Quick Checks (Docker)

Use these commands from `itii-assist-classroom-back`.

```bash
# All attendance-blocked queue events
docker compose -f docker-compose.backend.yml logs backend | rg "queue_(validate|create) attendance_blocked"
```

```bash
# Count by event type
Docker compose -f docker-compose.backend.yml logs backend \
  | rg "queue_(validate|create) attendance_blocked" \
  | awk '{print $0}' \
  | rg -o "queue_(validate|create)" \
  | sort \
  | uniq -c
```

```bash
# Inspect one student (replace 653xxxxxxx)
Docker compose -f docker-compose.backend.yml logs backend \
  | rg "attendance_blocked" \
  | rg "student_code=653"
```

## Suggested Triage Flow

1. Confirm `queue_validate attendance_blocked` appears before booking attempts.
2. Check whether `queue_create attendance_blocked` persists for more than 30 seconds for the same student/session.
3. If yes, verify attendance row update path and session linkage (`linked_attendance_session_id`) for the queue session.
4. If no, issue is likely stale client state and should be resolved by auto re-validation on the booking page.

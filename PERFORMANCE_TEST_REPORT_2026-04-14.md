# Performance Test Report - 2026-04-14

## เป้าหมาย

ทดสอบความสามารถของ backend สำหรับ flow ที่หนักที่สุดของระบบ คือ

- เช็คชื่อ (attendance check-in)
- จองคิว (queue booking)

โดยตั้งเป้าประเมินว่า backend ปัจจุบันรองรับการใช้งานพร้อมกันได้มากแค่ไหน และยัง "ลื่น" อยู่หรือไม่ เมื่อเทียบกับโจทย์ที่ต้องรองรับนักศึกษาจำนวนมากพร้อมกันจาก web app และ mobile app

## สภาพแวดล้อมที่ใช้ทดสอบ

- ระบบปฏิบัติการ: Windows
- Runtime: Go 1.26.2
- Framework: Fiber v3
- Database: PostgreSQL ในเครื่อง local
- เครื่องมือทดสอบโหลด: `tools/loadtest/main.go`
- orchestration script: `tools/stress_attendance_queue.ps1`
- จำนวนนักศึกษาสำหรับ stress test: 600 คน
- benchmark runtime config:
  - `APP_ENABLE_REQUEST_LOGGER=false`
  - `DB_MAX_OPEN_CONNS=250`
  - `DB_MAX_IDLE_CONNS=100`
  - PostgreSQL local มี `max_connections=100` จึงถูก cap อัตโนมัติเป็น `max_open_conns=80`, `idle=80`

## วิธีทดสอบ

1. login ด้วยบัญชี admin
2. สร้าง/นำเข้านักศึกษาทดสอบ 600 คน
3. enroll นักศึกษาเข้า section ที่ใช้ทดสอบ
4. สร้าง attendance session ใหม่ทุกครั้งที่รัน attendance test
5. สร้าง queue session ใหม่ทุกครั้งที่รัน queue test
6. ยิง request แบบ unique student ต่อ request เพื่อจำลองการใช้งานจริง
7. ใช้ fresh restart สำหรับการวัดที่ต้องการหา threshold แบบแม่นยำ

## สิ่งที่แก้ในโค้ด

### รอบแรก

- ลดการเขียน `queue_desk_statuses` สำหรับ help booking ที่เดิมเขียนซ้ำทุก booking
- ทำให้ public desk status derive สถานะจาก active bookings เมื่อเหมาะสม
- เพิ่ม composite indexes สำหรับ flow ที่ใช้บ่อย
- ทำให้ DB pool ปรับผ่าน env ได้ และ cap ตาม `max_connections` ของ PostgreSQL อัตโนมัติ
- เพิ่มสวิตช์ปิด request logger ระหว่าง benchmark

ไฟล์หลักที่เกี่ยวข้อง:

- `repositories/queue_repo.go`
- `handlers/queue_handler.go`
- `config/database.go`
- `cmd/api/main.go`

### รอบสอง

- เพิ่ม `next_queue_number` ใน `queue_sessions` เพื่อไม่ต้องหา `MAX(queue_number)` ทุกครั้ง
- เปลี่ยนการจองคิวให้ reserve หมายเลขคิวแบบ atomic ใน transaction
- เพิ่ม unique partial index สำหรับ active booking ต่อ student ต่อ session
- เพิ่ม index สำหรับ `queue_sessions(pin_code, status)` เพื่อช่วย public booking flow
- เพิ่ม startup compatibility migration เพื่อ sync `next_queue_number` จากข้อมูลเดิมใน `queue_bookings`

ไฟล์หลักที่เกี่ยวข้อง:

- `models/schema.go`
- `repositories/queue_repo.go`
- `config/database.go`
- `cmd/api/main.go`

## ผลการทดสอบ

### Baseline ก่อนแก้

| กลุ่มทดสอบ | ผ่านสูงสุดที่ยืนยันได้ | เริ่มล้มที่ | หมายเหตุ |
| --- | ---: | ---: | --- |
| Attendance | 219 | 220 | เริ่มมี connection-refused |
| Queue booking | 218 | 219 | p95 ใกล้ 1.2s แล้วก่อนล้ม |

### หลังแก้รอบแรก

| กลุ่มทดสอบ | ผ่านสูงสุดที่ยืนยันได้ | เริ่มล้มที่ | หมายเหตุ |
| --- | ---: | ---: | --- |
| Attendance | 270 | 275 | 275 ล้มซ้ำหลาย fresh runs |
| Queue booking | 299 | 300 | 299 ผ่านใน fresh run, 300 เริ่มล้ม |

หมายเหตุ:

- Attendance ดีขึ้นจาก 219 -> 270
- Queue ดีขึ้นจาก 218 -> 299

### หลังแก้รอบสอง (queue counter + unique/index)

| กลุ่มทดสอบ | ผ่านสูงสุดที่ยืนยันได้ | เริ่มล้มที่ | หมายเหตุ |
| --- | ---: | ---: | --- |
| Attendance | 270 (conservative) | 275+ | รอบสองไม่ได้โฟกัส attendance โดยตรง |
| Queue booking | 300 | 310 | 300 ผ่านครบใน fresh run, 310 ล้ม |

รายละเอียดรอบสำคัญ:

- `300` concurrent queue booking: `success=300`, `failures=0`, `p95_ms=2048.98`
- `310` concurrent queue booking: `success=231`, `failures=79`
- `275` concurrent attendance: ล้มซ้ำหลายครั้ง
- หลังจาก queue ล้มที่ขอบ ระบบยังตอบ `GET /api/health` ได้ `200` แปลว่าเป็นภาวะ saturation ใกล้เพดาน ไม่ใช่ crash ค้างถาวร

## สรุปเชิงวิศวกรรม

### เพดานทางเทคนิคที่วัดได้ล่าสุด

- Attendance: ใช้ `270 concurrent` เป็นเพดานที่เชื่อถือได้ตอนนี้
- Queue booking: ใช้ `300 concurrent` เป็นเพดานที่เชื่อถือได้ตอนนี้

### เพดานที่ถือว่า "ลื่น"

ถ้าเอาประสบการณ์ใช้งานจริงมาคิดร่วมด้วย ไม่ควรใช้เพดานทางเทคนิคเป็นค่าใช้งานจริง เพราะ latency ใกล้ขอบจะสูงมาก

- Attendance แนะนำให้อยู่ไม่เกินประมาณ `250-260 concurrent`
- Queue booking แนะนำให้อยู่ไม่เกินประมาณ `250-280 concurrent`

เหตุผล:

- queue ที่ `300` แม้ผ่านครบ แต่ p95 ประมาณ `2.05s` ยังไม่ถือว่า "ลื่น"
- queue ที่ช่วง `260-280` ยังใช้งานได้ดีกว่าในเชิง UX

## เทียบกับเป้าหมาย 500 concurrent

สถานะปัจจุบัน: **ยังไม่ถึงเป้าหมาย 500 concurrent สำหรับ flow หนักของระบบ**

โดยเฉพาะถ้าความหมายของโจทย์คือ

- ไม่ค้าง
- ไม่หน่วง
- ใช้งานลื่น

ตอนนี้ backend หลังปรับแล้วดีขึ้นชัดเจน แต่ยังไม่พอสำหรับ 500 concurrent ใน flow เช็คชื่อ/จองคิว บนสภาพแวดล้อม local single-node นี้

## สิ่งที่ยืนยันแล้ว

- `go build ./...` ผ่านหลังแก้โค้ด
- benchmark script เดิมยังใช้งานได้
- compatibility migration สำหรับ queue counter ทำงานตอน startup
- server ยังตอบ health check ได้หลังชนเพดาน queue

## ความเสี่ยงและข้อจำกัด

- การทดสอบนี้เป็น local single-node test ไม่ใช่ production deployment
- ไม่มี automated Go test suite ใน repo ปัจจุบัน จึงใช้ build + smoke/load test เป็นหลัก
- attendance script ปัจจุบันรันคู่กับ queue ใน orchestration เดียวกัน จึงเหมาะกับ capacity validation มากกว่า micro-benchmark แบบแยก flow ล้วน

## ข้อเสนอแนะถัดไป

1. แยก worker/service สำหรับ queue booking ออกจาก path synchronous บางส่วน ถ้าต้องการดันเข้าใกล้ 500 จริง
2. เก็บ metrics ของ app และ PostgreSQL ระหว่างเทสต์ เช่น CPU, memory, DB waits, active connections
3. แยก stress script ให้รัน attendance-only และ queue-only ได้ เพื่อหาคอขวดของแต่ละ flow ให้แม่นขึ้น
4. ถ้าจะรองรับ 500 พร้อมกันจริง ควรวางแผน multi-instance + reverse proxy/load balancer + production-grade PostgreSQL tuning

## ไฟล์ที่เกี่ยวข้องกับการแก้ล่าสุด

- `models/schema.go`
- `repositories/queue_repo.go`
- `config/database.go`
- `cmd/api/main.go`
- `tools/stress_attendance_queue.ps1`
- `tools/loadtest/main.go`

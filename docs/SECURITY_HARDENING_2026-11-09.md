# รายงานการแก้ความปลอดภัยและตรวจผลกระทบ — 9 พฤศจิกายน 2026

ดำเนินการใน source ของ backend, frontend และ deployment ตามคำสั่งให้ทำข้อ 2–5 พร้อมตรวจ regression ไม่ได้ deploy, push หรือแก้ข้อมูล production

**สถานะ: ลงโค้ดและทดสอบบางส่วนแล้ว ยังยืนยันไม่ได้ว่าทุก flow ใช้งานเหมือนเดิม และยังไม่ผ่านเกณฑ์พร้อมขึ้น production** เพราะ environment รัน frontend build, integration suite และระบบผ่าน container จริงไม่ได้ รายงานนี้แยกสิ่งที่ผ่านจริงออกจาก test ที่เพิ่มไว้แต่ยังไม่ได้รัน

## สิ่งที่เปลี่ยน

| พื้นที่ | การแก้ | ผลต่อผู้ใช้ที่ตั้งใจให้เปลี่ยน |
|---|---|---|
| เช็กชื่อ | start-session ต้องผ่านสิทธิ์รายวิชา/สิทธิ์แก้ attendance; ตรวจบัญชีนักศึกษายัง active ก่อนเช็กชื่อและก่อนคืนผล idempotency | TA ต่างวิชาและบัญชีระงับใช้ทางลัดนี้ไม่ได้ |
| จองคิว | protected route, ใช้ student identity จาก session, เช็กเจ้าของก่อนอ่าน/ยกเลิก; staff ต้องมีสิทธิ์วิชานั้น; legacy route ใช้ validation กฎคิวเดียวกัน | นักศึกษาต้อง login และจองเป็นคนอื่นไม่ได้; staff ที่มีสิทธิ์ยังช่วยจองได้ |
| ควบคุมคิว | status/cutoff/projector heartbeat ต้อง login และผ่าน permission | projector ที่ไม่ login ยังอ่านได้ แต่ควบคุม session ไม่ได้; frontend ไม่ส่ง heartbeat จากผู้ชมทั่วไป |
| Session | ตรวจ JTI, อายุ token, refresh record, account active และ role ปัจจุบันทั้ง HTTP และ socket | ปิดบัญชี/เพิกถอน session แล้ว token เก่าใช้ต่อไม่ได้; legacy token ที่ไม่มี JTI ต้อง login ใหม่ |
| JWT/CSRF | HS256 เท่านั้น, parse subject/expiry อย่างปลอดภัย; cookie refresh/logout ต้องมี CSRF header | Bearer/mobile ยังใช้เส้นทางเดิม; cookie client ต้องส่ง CSRF token |
| 2FA | opaque first-factor challenge อายุ 5 นาที, เก็บ hash; OTP ผูกกับ challenge, จำกัด 5 ครั้ง, single use, transaction lock, ป้องกันใช้ TOTP ซ้ำต่าง challenge, recovery code ใช้ครั้งเดียว | userId+OTP อย่างเดียวเข้าไม่ได้; password และ OAuth ส่ง challenge ให้หน้า verify; รองรับ verify แล้ว complete และ complete โดยตรง |
| Login rate limit | account 20 ครั้ง/5 นาที; IP default 6,000 ครั้ง/นาทีปรับด้วย AUTH_MAX_ATTEMPTS_PER_IP_PER_MINUTE; Redis shared และ memory fallback | เผื่อ campus NAT ที่มีหลายคนใช้ IP เดียว; ยังต้อง load test ยืนยันค่าจริง |
| WebSocket | private room ต้องมี ticket ที่ผูก identity, ตรวจ expiry/revocation/permission เป็นระยะ; public queue feed ตัด PII; client ส่ง data-change/course-change/classroom-change ปลอมไม่ได้ | หน้า staff รับข้อมูลเต็มผ่าน private room; backend ส่ง invalidation หลัง mutation สำเร็จแทน client |
| ขีดจำกัด WebSocket | 64 KiB/frame, 60 frames/วินาที, 64 rooms/socket; connection default 8,192/process และ 4,096/IP; fallback ticket store มีเพดาน | ปรับได้ด้วย WEBSOCKET_MAX_CONNECTIONS และ WEBSOCKET_MAX_CONNECTIONS_PER_IP; ต้องทดสอบกับปริมาณผู้ใช้จริง |
| ไฟล์ภาพ | ตรวจ decoded format/pixel count ไม่เชื่อ MIME/นามสกุล; re-encode JPEG; ตัด metadata/active tail; ไม่ fallback ไปเก็บ original | JPG/PNG/GIF/WebP ที่ถูกต้องยังอัปโหลดได้; ภาพเคลื่อนไหว/ความโปร่งใสกลายเป็น JPEG; SVG/AVIF สำหรับ upload ใหม่ถูกปฏิเสธ; เกิน 25 ล้าน pixels ถูกปฏิเสธ |
| หลักฐานคะแนน | เก็บไฟล์ใหม่ใต้ private; URL เดิมผ่าน route ตรวจเจ้าของ/ผู้ตรวจคะแนน; legacy file อ่านได้ผ่านสิทธิ์เดียวกัน; bypass shared cache | ส่งลิงก์ตรงให้คนไม่มีสิทธิ์อ่านไม่ได้; frontend fetch เป็น blob รองรับ session refresh; preview ไฟล์ในเครื่องยังคงเดิม |
| XSS/ไฟล์ public | escape ชื่อห้อง/อาคาร/ชั้นใน HTML พิมพ์ QR; จำกัดชนิด static upload; nosniff และ CSP sandbox บน upload; proxy ปฏิเสธ path traversal | พิมพ์ QR ยังใช้ layout เดิม; ไฟล์ active content ไม่ถูกเสิร์ฟเป็นหน้าเว็บที่รัน script |
| Push | ลงทะเบียน subscription ต้อง login; booking target ต้องเป็นเจ้าของ/ผู้มีสิทธิ์ | ไม่สามารถผูก push ไปหาคิวของคนอื่นโดยเดา ID |
| CORS/Proxy | explicit origin allowlist; ไม่ trust ทุก private IP; API standalone bind loopback; socket proxy แทน mount Docker socket เข้า API | ต้องตั้ง CORS_ALLOWED_ORIGINS และ trusted proxy ให้ตรง deployment |
| Framework | Next 16.4.3 → 16.4.5; Fiber 3.1.0 → 3.2.0; อัปเดต transitive ที่จำเป็น; x/image → 0.43.0 พร้อม x/text/x/sync ที่เข้ากัน | ต้อง build frontend/backend พร้อมกัน; ยังไม่ได้ยืนยัน artifact install ใน environment นี้ |

## ผลที่รันจริง

ผลแบบ machine-readable: [security-verification-2026-11-09.json](security-verification-2026-11-09.json)

| การตรวจ | ผล | ขอบเขต/ข้อจำกัด |
|---|---|---|
| Go build ./config ./realtime ./repositories | ผ่าน | ใช้ source จาก official tags ของ dependency ใหม่ผ่าน temporary modfile; ไม่ใช่ full API build |
| Go tests ./utils | ผ่าน 25 test cases/subtests | รวม JWT malformed claims/algorithm, origin/proxy allowlist และภาพปลอม/ภาพเสีย/pixel bomb/strip trailing content |
| Realtime tests ที่ไม่ต้องใช้ Redis | ผ่าน 15 tests | รวม bare-ID room rejection, private ticket rejection, forged client update, public PII filtering, room/connection caps, origin checks; ไม่รวม 2 ไฟล์ Redis tests |
| ตรวจ uploads เดิมแบบ read-only | 164 ไฟล์, findings 0, modified_files 0 | ตรวจ extension, size, raster dimensions และ PDF signature; ไม่ใช่ malware scan หรือการรับรองทุก byte |
| Go source parse/import/context checks | ผ่าน | ตรวจ syntax/import usage แบบ static; ไม่เท่ากับ compile handlers ทั้ง package |
| package.json ↔ package-lock.json | ผ่าน | root dependency maps ตรงกัน; Next/env/SWC 10 entries เป็น 16.4.5 พร้อม URL/integrity; ไม่ใช่ npm ci |
| Docker Compose config --quiet | ผ่านทั้ง 2 ชุด | ใช้ placeholder origin/password สำหรับตรวจ syntax; main compose เตือน tunnel token ไม่ได้ตั้ง; ไม่ได้เริ่ม service |
| YAML parse, bash -n scripts, git diff --check | ผ่าน | ไม่ใช่ nginx -t หรือ browser test |

เครื่องนี้ใช้ Go 1.27.3 ทดสอบ package ที่เข้าถึง dependency ได้ ส่วน Dockerfile ยังใช้ Go 1.26 ตามเดิม ต้องยืนยัน full build ด้วย toolchain ที่ deploy จริงอีกครั้ง

Dependency ใหม่ 7 โมดูลใช้ source tree จาก official version tags ใน /tmp เพื่อให้ทดสอบส่วน core ได้โดยไม่แตะ go.mod ด้วย local replace: Fiber, schema, utils/v2, msgp, x/image, x/text, x/sync ไม่มี temporary replace อยู่ใน go.mod ของโปรเจกต์

ข้อจำกัด dependency: ยัง download module zip และตรวจ Go checksum database ตามกระบวนการปกติไม่ได้ โดยเฉพาะ root zip hashes ของ Fiber 3.2.0 และ x/image 0.43.0 ยังไม่ได้เติมด้วย go mod download จริง จึงต้องทำขั้นตอนนั้นก่อนใช้ -mod=readonly; ไม่ได้สร้าง hash ของ zip ขึ้นเอง Next lock integrity เทียบจาก pinned lockfiles สองแหล่งและโครงสร้าง dependency ของ official Next tag แล้ว แต่ยังไม่ผ่าน npm ci

## ชุดทดสอบที่เพิ่มแล้วแต่ยังรันไม่ผ่านขั้น setup

routes/security_regression_test.go ใช้ SQLite fixture และ actual routes ครอบคลุม:

- anonymous จอง/ยกเลิก/อ่าน booking หรือควบคุมคิวไม่ได้
- นักศึกษาปลอม student ID, อ่าน/ยกเลิก booking ผู้อื่น, ควบคุมคิว หรือขอ ticket ของผู้อื่นไม่ได้ และ DB ไม่เปลี่ยน
- เจ้าของยังอ่านสถานะและขอ socket ticket ได้
- TA start attendance ต่างวิชาไม่ได้; วิชาที่อนุญาตผ่าน authorization ไปถึง readiness check
- บัญชี disabled และ session revoked เข้า private data ไม่ได้
- password → email 2FA challenge → verify → complete และปฏิเสธ replay/ไม่มี challenge

**ยังไม่นับรายการนี้เป็นผลผ่าน**: go test ./... หยุดระหว่าง setup เพราะ dependency เดิมบางรายการไม่มีใน cache (เช่น SQLite, miniredis, otp, oauth2, minio, webpush, gopsutil) และ shell ไม่มี network DNS ใช้งาน; cache ระบบเป็น read-only จึงดาวน์โหลดไม่ได้ ยังไม่รู้ว่ามี compile error หรือ integration regression ใน handlers/main เหลือหรือไม่

Frontend ไม่มี Node/npm จึงยังไม่ได้ npm ci, TypeScript check, production build หรือ browser E2E ส่วน Go race test ไม่มี C compiler และไม่มี runtime สิทธิ์ใช้งาน Docker/Nginx สำหรับ smoke test deployment

## วิธีตรวจต่อแบบทำซ้ำได้

บน disposable development/CI checkout ที่มี network และ toolchain:

```bash
# backend (Go 1.26.2+, gcc สำหรับ race)
bash scripts/verify-security.sh

# frontend (Node/npm ที่ Next 16 รองรับ)
bash scripts/verify-security.sh
```

Backend script ทำ mod download/verify, full tests, vet, API/seed-admin build และ race tests; frontend script ทำ npm ci, typecheck, build, audit production dependencies สคริปต์ไม่ deploy และไม่ชี้ production database

สำหรับ inventory ไฟล์เดิมแบบไม่แก้ไฟล์:

```bash
go run ./tools/audit-uploads ./uploads
```

## Regression ที่ต้องยืนยันบน staging ก่อน rollout

| สถานการณ์ | เกณฑ์ผ่าน |
|---|---|
| Login ทุก provider | local, Google, GitHub, KKU และ student login สำเร็จ; redirect กลับหน้าที่มาได้ |
| 2FA | TOTP/email/recovery ผ่านเมื่อถูกต้อง, รหัสผิดแสดง error โดยไม่วน refresh, หมดอายุให้ login ใหม่, resend cooldown ทำงาน |
| เช็กชื่อจริง | นักศึกษา active/ลงทะเบียน/PIN ถูก/เวลาและnetworkถูก ผ่านได้, QR/PIN หมุนและ auto-close ยังทำงาน, duplicate check-in ไม่เพิ่ม record |
| เช็กชื่อผิดเงื่อนไข | ต่างวิชา, disabled, PIN เก่า/ผิด, หลังปิด, spoof forwarding headers ถูกปฏิเสธตามกฎเดิม |
| จองคิว | login แล้ว scan QR, validate, create, reload restore, status realtime/polling, cancel ของตัวเอง; help/grading/late cutoff/attendance-linked/concurrent groups ครบ |
| หน้าจอคิว | anonymous อ่านภาพรวมได้, ไม่มี student PII; staff เห็น identity และ controls; pause/resume/cutoff/heartbeat ทำงาน |
| หลักฐานคะแนน | author/reviewer อ่านภาพเก่าและใหม่ได้หลัง access token หมดอายุ, คนอื่นถูกปฏิเสธ, local preview/approve/reject ไม่เสีย |
| Upload | avatar/course cover/announcement/evidence ภาพปกติแสดงได้; แสดงข้อความเมื่อชนิด/ขนาดไม่รองรับ; รับทราบ JPEG flattening |
| Realtime | เปิด staff 2 browser เปลี่ยน course/assignment/section/team/score/attendance/classroom แล้วอีกจอ refresh; logout/disable/revoke ปิด private socket ภายในประมาณ 25 วินาที |
| Session | HTTP cookie refresh/logout/CSRF และ mobile Bearer ใช้ได้; role เปลี่ยนแล้วต้องไม่ค้างสิทธิ์เดิม |
| Push | subscribe/notify/unsubscribe ของเจ้าของทำงาน; ไม่ได้ notification ของคนอื่น |
| Load | 1,000 students บน NAT เดียว, เช็กชื่อ/จองพร้อมกัน; เปรียบเทียบ p95 latency, 4xx/5xx, DB pool, Redis, CPU/RAM กับ baseline |

## ข้อควรทำในการ deploy และ rollback

1. เก็บ backup DB/uploads และ image/config เก่า แล้วผ่าน full tests/build ข้างต้นก่อนเปลี่ยน traffic ไม่มีการทำขั้นตอนนี้บน production ในงานรอบนี้
2. ตรวจ CORS_ALLOWED_ORIGINS เป็น origin จริงของทุก frontend (ไม่มี wildcard) ตั้งใน environment ที่ Compose ใช้ interpolate; main compose ใช้ .env ของ deployment ไม่ใช่แค่ env_file ของ backend ต้องยืนยัน cookie/CSRF บนทุกโดเมน
3. AutoMigrate เพิ่ม login_challenges แบบ additive ไม่ลบ schema เดิม หาก rollback ให้เก็บตารางนี้ไว้ได้
4. Compose เพิ่ม api_ingress 172.30.248.0/24 และ tunnel_ingress 172.30.249.0/24 โดยไม่ renumber app/web เดิม ตรวจ subnet ไม่ชน network ของ host; nginx backend aliases ต้องอยู่ network ใหม่นี้ก่อนสลับ upstream รวมถึง slot ที่จะใช้ rollback
5. Backend trust nginx เฉพาะ 172.30.248.2/32; nginx trust tunnel เฉพาะ 172.30.249.3/32 ต้องตรวจ c.IP()/network guard ด้วย request จริงผ่าน KKU และ Cloudflare หาก ingress ผิดจะทำให้เช็กชื่อถูกปฏิเสธหรือ rate limit รวมเป็น IP เดียว
6. Rollout backend/frontend/nginx ให้สอดคล้องกัน: frontend เก่าไม่มี 2FA challenge และ private room tickets; backend ใหม่จะปฏิเสธตามตั้งใจ หลีกเลี่ยง mixed versions ระหว่างช่วงใช้งาน
7. ล้าง CDN/edge cache ของ /api/uploads/score-edit-requests/* ที่อาจถูก cache ไว้ก่อนการแก้; origin cache bypass ใหม่ไม่ลบสำเนาเก่าที่ edge อัตโนมัติ
8. Mount uploads volume ให้ถูกต้อง (Docker image ใหม่ไม่บรรจุ uploads/secret env); หลักฐานใหม่อยู่ uploads/private/score-edit-requests แต่ URL เดิมไม่เปลี่ยน หาก rollback backend เดิมต้องเตรียมการอ่านไฟล์ private ใหม่ด้วย ไม่เช่นนั้นหลักฐานที่อัปโหลดหลัง rollout จะอ่านจาก backend เก่าไม่ได้
9. socket session revocation ตรวจเป็นช่วง ping ประมาณ 25 วินาที ไม่ใช่ทันที; connection/rate caps เป็นค่าต่อ process/fallback มีขอบเขตตาม replica ต้องวัดก่อนใช้งานจริง

## ขอบเขตการป้องกันการลักไก่ที่ยังเหลือ

การผูก identity และตรวจสิทธิ์ช่วยปิดการปลอมรหัส/เดา ID/รับข้อมูล PIN จาก private socket แต่ไม่ใช่หลักฐานว่าคนนั้นอยู่หน้าห้องเสมอ นักศึกษายังอาจแชร์บัญชี/OTP หรือส่ง PIN ให้กัน, GPS/device hints ฝั่ง client ปลอมได้ และ IP เป็นเพียงสัญญาณ network ไม่ใช่ตัวบุคคล ยังไม่ได้เพิ่ม biometric, hardware attestation หรือเปลี่ยนนโยบายเช็กชื่อทั้งหมด

ยังไม่ได้ทำ full dependency/SBOM vulnerability scan สำเร็จ และไม่ได้รับรองว่าโปรเจกต์ไม่มีช่องโหว่อื่น การ escape จุดพิมพ์ QR และ image re-encoding ไม่ใช่การรับรองว่า XSS ทุกเส้นทางถูกปิดทั้งหมด

## เอกสารอ้างอิงของ patch

- [Next SVG advisory](https://github.com/advisories/GHSA-7qx7-33qx-rqx2), [response framing](https://github.com/advisories/GHSA-pc58-hwjq-p385), [cache binary arguments](https://github.com/advisories/GHSA-rhh4-g77r-r9qj)
- [Fiber AutoFormat advisory](https://github.com/advisories/GHSA-qjv7-627w-8qjv), [cache advisory](https://github.com/advisories/GHSA-35hp-hqmv-8qg8): ในแอปไม่พบใช้ API ที่ได้รับผลโดยตรง แต่ปรับ patch/framework เพื่อลด exposure
- [Go image WebP advisory GO-2026-5061](https://pkg.go.dev/vuln/GO-2026-5061)
- Official tagged source: [Next v16.4.5](https://github.com/vercel/next.js/tree/v16.4.5), [Fiber v3.2.0](https://github.com/gofiber/fiber/tree/v3.2.0), [x/image v0.43.0](https://github.com/golang/image/tree/v0.43.0)

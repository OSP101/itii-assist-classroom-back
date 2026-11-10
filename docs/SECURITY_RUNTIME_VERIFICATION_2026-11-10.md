# ผลตรวจซ้ำด้วย Docker — 10 พฤศจิกายน 2026

ผลรอบล่าสุดที่แก้ audit เหลือ 0 และเพิ่ม OAuth/2FA/key-rotation checks อยู่ใน [SECURITY_RELEASE_VERIFICATION_2026-11-10.md](SECURITY_RELEASE_VERIFICATION_2026-11-10.md) รายงานด้านล่างเป็นผลรอบก่อนหน้า

รอบนี้รันในสำเนา source และระบบทดสอบแยกที่ `/tmp/itii-verify-20261110` ไม่ deploy, push, restart บริการจริง หรือเชื่อมฐานข้อมูล/Redis/uploads ของ production ระบบทดสอบใช้ PostgreSQL 16 และ Redis 7 ใหม่ บัญชีจำลอง และ Docker network แบบ internal

## ผลที่ยืนยันแล้ว

| การตรวจ | ผล |
|---|---|
| Go module checksum verification | ผ่าน โดยไม่มี temporary module replacements |
| `go test -mod=readonly -count=1 -json ./...` | ผ่าน 247 top-level tests รวม subtests เป็น 323 รายการ ใน 10 packages ที่มี tests |
| `go vet -mod=readonly ./...` | ผ่าน |
| Race checks: utils, realtime, routes, repositories | ผ่านทั้ง 4 packages |
| API และ seed-admin binaries, CGO_ENABLED=0 | build ผ่านด้วย Go 1.26.8 |
| Frontend `npm ci` | ผ่านด้วย Node 22.23.4 |
| Frontend security regression tests | ผ่าน 6/6 |
| TypeScript | ผ่าน หลังแก้ compile errors 4 จุด |
| Production build: Next 16.4.5 / webpack | ผ่าน รวมสร้าง static pages 63 หน้า |
| Compose config: backend และ deployment | ผ่าน; deployment เตือน tunnel token ไม่ได้ตั้ง |
| Nginx 1.30.6 config test | ผ่าน โดย render template ด้วยค่าทดสอบและ certificate ที่สร้างใหม่ |
| Source comparison | SHA-256 ตรงกันทั้ง frontend 452 และ backend 227 source/config files รวม 679 ไฟล์ |
| Bash syntax | ผ่าน 17 scripts |
| Dockerfile builds | ผ่านทั้ง frontend และ backend ใช้ image tags แยกจากระบบจริง |
| ทดสอบ runtime บน images สุดท้าย + Nginx + PostgreSQL + Redis | ผ่าน 41/41: browser/API 21, attendance/queue 14, private evidence/proxy 6 |

ชุด Go เหลือ skip 1 subtest: `TestTouchHintsDoNotLetANonMacDesktopThrough/macbook_running_chrome` ซึ่ง source ระบุเป็นข้อจำกัดเดิมที่ยอมรับไว้: Mac ที่ปลอม touch hints อาจผ่านเงื่อนไข device ได้ ไม่ใช่กรณีที่ทดสอบผ่าน

Browser ใช้ Chromium ผ่าน Playwright: password login → admin → classrooms, ไม่มี uncaught browser exceptions หรือ application chunks ที่ตอบ error; WebSocket ได้ `socket-ready`; anonymous booking เก็บ return path ไป student login และ anonymous projector ไม่ถูก redirect ไป login

API smoke ครอบคลุม anonymous queue rejection, 2FA ที่ไม่มี challenge, private evidence/no-store, active upload extension, secure/httpOnly cookies, profile, CSRF rejection บน refresh/logout, CSRF rotation, old access token ถูกเพิกถอนหลัง refresh, cookie logout และ mobile Bearer login/refresh/logout, CORS ของ origin ที่ไม่อนุญาต

เพิ่ม fixture รายวิชา/ห้องเรียน/นักศึกษาใน PostgreSQL ทดสอบ และออก student/instructor session ด้วย JWT utilities จริงพร้อม refresh records เพื่อทดสอบหลัง login โดยไม่ได้เรียก identity provider ภายนอก: instructor เปิด attendance ด้วย Redis จริง, PIN ผิดถูกปฏิเสธ, นักศึกษาที่ลงทะเบียนเช็กชื่อได้, ส่งซ้ำแล้วฐานข้อมูลมี attendance record เดียว, ปลอม student identity ไม่ได้, จอง/อ่าน/ยกเลิกคิวตนเองได้, คนอื่นอ่านหรือยกเลิกไม่ได้, pause/resume ใช้ได้, paused queue ไม่รับจอง และ anonymous push registration ถูกปฏิเสธ หลังจบไม่มี active booking ค้างใน fixture

Private evidence ตรวจทั้ง authorized reviewer และผู้ไม่มีสิทธิ์ ผ่าน Nginx และ Next upload route: ส่ง Bearer/cookie identity ได้, response เป็น private/no-store และการอ่านสำเร็จด้วยสิทธิ์หนึ่งไม่ทำให้ anonymous อ่านผ่าน cache ได้ ตรวจ authenticated profile reads พร้อมกัน 20 คำขอผ่านทั้งหมด

## สิ่งที่แก้ระหว่างตรวจ

- เติม checksum ที่ตรวจผ่าน Go ตามปกติสำหรับ Fiber 3.2.0 และ x/image 0.43.0 ใน `go.sum` เพื่อให้ `-mod=readonly` ใช้งานได้
- หน้า queue booking: ลบ `readOnly` ที่อ้าง user ใน branch ที่ยืนยันแล้วว่าไม่มี user
- หน้า projector: อ่าน error เป็น string ตาม API client จริง แก้ TypeScript errors อีก 3 จุดและแสดงข้อความตอบกลับได้ถูกต้อง
- เพิ่ม test เปิดหลักฐานคะแนนทั้ง private path ใหม่และ legacy path พร้อมตรวจ headers; ผ่านโดยไม่ต้องแก้ handler
- เปลี่ยน fixture ของ attendance tests ให้ใช้ SQLite datetime dialect ที่มีอยู่แล้ว, แยก in-memory database ต่อ test, ปิด connection หลังจบ และกำหนด pin mode เหมือนเส้นทางสร้าง session จริง ทำให้ 8 tests ที่เคย skip ได้รันจริง
- ตั้ง timeout ของ HTTP test harness เป็น 10 วินาที เพื่อรองรับ password hashing ภายใต้ race instrumentation; ไม่เปลี่ยน timeout ของระบบจริง

## ข้อที่ยังไม่ผ่าน/ขอบเขตที่ยังไม่รับรอง

`npm audit --omit=dev --json` ยังรายงาน 62 รายการ: critical 1, high 6, moderate 5, low 50 โดย critical อยู่ที่ transitive DOMPurify ผล audit นี้ยังไม่ผ่าน ไม่ได้ใช้ `npm audit fix --force` หรือเปลี่ยน major dependency ในงานตรวจ regression รอบนี้

ยังไม่ได้ทดสอบ OAuth/KKU/Google/GitHub กับบัญชีจริง, การส่ง email/push ไปผู้รับจริง, เครือข่าย campus/university proxy/Cloudflare จริง, full browser journey ของ check-in/queue ที่มีข้อมูลรายวิชาจริง หรือโหลดระดับผู้ใช้จำนวนมาก ผล functional และ race ที่ผ่านไม่ใช่การรับรองว่าไม่มีผลกระทบทุกกรณี หรือพร้อม production โดยไม่มีเงื่อนไข

Build มี warnings เดิมเรื่อง `experimental.useCache` deprecated และ React peer dependency ของ react-konva; การดาวน์โหลด Google Fonts พบ DNS retry แต่ build สุดท้ายสำเร็จ ไม่ได้เปลี่ยน dependency major หรือ configuration เหล่านี้ในรอบนี้

ผลแบบ machine-readable: [security-runtime-verification-2026-11-10.json](security-runtime-verification-2026-11-10.json)

หลักฐานดิบอยู่ที่ `/tmp/itii-verify-20261110`: `frontend.log`, `frontend-image-final.log`, `backend-image.log`, `backend-final.log`, `itii-assist-classroom-back/final-tests.jsonl`, `final-runtime.log`, `runtime-results.json`, `flow-results.json`, `evidence-results.json`, `source-comparison.json`, `login.png`, `admin.png` และ `itii-assist-classroom-front/audit-verification.json`

Images ที่ตรวจจริง:

- `itii-verification-backend:20261110`: `sha256:0a2cb784b787cbbdb6a66796b1e114cb7b59e24bbf239fde87d1f249f5ee7b96`
- `itii-verification-frontend:20261110`: `sha256:62461009ef87aff7f9ea7ef46a55320c8c08b0942df3eec1338705f630495a41`

ตรวจบริการเดิมแบบ read-only: API health และ login ตอบ 200; Nginx, active frontend/backend replicas, PostgreSQL และ Redis ยัง healthy โดยเวลาเริ่ม container ยังเป็นวันที่เดิมก่อนงานนี้ ไม่ได้สลับ slot หรือเปลี่ยน image tags ของ deployment จริง

หลังตรวจเสร็จ ลบ containers/database/network ของ stack ทดสอบแล้ว และล้าง Go/npm/Next build caches ชั่วคราวในสำเนาทดสอบ เก็บรายงาน raw logs, screenshots, source snapshot และ images ที่ระบุข้างต้นไว้ตรวจสอบย้อนหลัง

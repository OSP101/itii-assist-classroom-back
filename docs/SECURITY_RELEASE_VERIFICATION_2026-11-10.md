# Release verification — 10 พฤศจิกายน 2026

**ผลฝั่งโค้ดและระบบทดสอบแยก: ผ่าน** ไม่ได้ deploy, สลับ slot, เปลี่ยน application env จริง หรือแก้ข้อมูลจริง การย้าย encryption key บน production ยังไม่ได้ apply

## Dependency audit ปิดครบ

ติดตั้งใหม่ด้วย `npm ci`, รัน `npm audit --json` และ `npm audit --omit=dev --json`: ทั้งสองชุดรายงาน **0 vulnerabilities** ไม่ใช้ `--force`, ไม่ปิด audit และไม่มี advisory ignore list

| Dependency | ก่อน | หลัง |
|---|---|---|
| DOMPurify | 3.4.5 | 3.4.17 |
| PostCSS | 8.5.14 | 8.5.32 |
| uuid ของ ExcelJS | 8.3.2 | 11.1.1 ผ่าน scoped override |
| postcss-selector-parser ของ typography | 6.0.10 | 6.1.6 ผ่าน scoped override |
| source-map-js | 1.2.1 | 1.2.2 |
| js-yaml | 3.14.2 | 3.15.2 |
| nanoid | 3.3.12 | 3.3.20 |
| tmp | 0.2.5 | 0.2.7 |

อัปเดต brace-expansion และ dependency อื่นที่มี patch ด้วย lockfile ใหม่ คง Next 16.4.5 และ ExcelJS 4.4.0 ไว้

deepmerge 4.3.1 เป็นต้นเหตุของ low findings ที่ส่งต่อผ่าน HeroUI หลายแพ็กเกจ Registry ยังไม่มี upstream รุ่นแก้ในรอบตรวจนี้ จึงใช้ local fork `@itii/deepmerge` เวอร์ชัน `4.3.1+itii.1` พร้อม MIT license และ type declarations เดิม แพตช์ปฏิเสธ `__proto__`, `constructor`, `prototype` ก่อนอ่าน/เขียนทั้ง target/source loops มี regression tests สำหรับ prototype poisoning และการทำงานปกติ ไม่ได้เปลี่ยนหมายเลข upstream เพื่อหลบ audit รายละเอียดอยู่ใน frontend `vendor/deepmerge/README.md` และ [รายงานต้นเหตุ upstream](https://github.com/TehShrike/deepmerge/issues/273)

เพิ่มการตรวจ Excel XLSX roundtrip, extended conditional formatting ที่ใช้ uuid, PDF generation, HeroUI theme และ merge behavior สคริปต์ CI frontend ตรวจ audit ทั้ง dev และ production dependencies และ backend build เครื่องมือย้าย key พร้อม race checks 5 packages

## OAuth และ 2FA

พบและยืนยันว่าระบบเดิมยอมรับ signed OAuth state ใน browser อื่น และไม่มี expiry ใน payload แก้ Google/GitHub ให้ state ผูกกับ provider, มีอายุ 5 นาที, ตรวจ nonce cookie ของ browser ที่เริ่ม flow, ตั้ง Secure/HttpOnly/SameSite=Lax, clear cookie หลัง callback และ no-store การผูก transaction กับ browser อ้างอิง [OAuth Security BCP, RFC 9700 §4.7](https://www.rfc-editor.org/rfc/rfc9700.html#section-4.7)

แก้ account linking ของ Google/GitHub/KKU ให้ยอมรับเฉพาะ staff principal และตรวจ session ปัจจุบันทั้งก่อนเริ่ม flow และตอน callback ป้องกัน student token ที่ ID ตรงกับ staff และ session ที่ revoked/disabled/เปลี่ยน role ระหว่าง flow มี regression tests ยืนยันการปฏิเสธและยืนยันว่า staff ปกติยังผูกบัญชีได้

ทดสอบ callback ด้วย controlled HTTP transport: Google/GitHub แลก code และอ่าน identity สำเร็จ, staff session ใช้ `/auth/me` ได้, ผู้เปิด 2FA ได้ challenge โดยยังไม่ได้ session; Google student ที่ลงทะเบียนและ active ได้ student principal ส่วน disabled/unverified ถูกปฏิเสธ ไม่มี request ออกไปหา IdP จริงจาก tests เหล่านี้

ทดสอบบน images จริงผ่าน Nginx: provider callback URL เป็น public HTTPS origin ถูกต้อง, browser อื่น/state หมดอายุ/replay ถูกปฏิเสธ, cancellation กลับหน้าที่ถูกต้อง, KKU nonce เริ่ม flow และปฏิเสธ callback ที่ไม่มี nonce

Browser ทดสอบ password → TOTP → admin จริงบนบัญชีจำลอง รวม recovery code, challenge/TOTP/recovery-code replay, 5-attempt lockout และการไม่ออก session ก่อนยืนยัน OTP ผ่านทั้งหมด

## Encryption key ของ TOTP เดิม

ตรวจ production แบบ read-only พบ deployed backends ไม่มี explicit `TWO_FACTOR_ENCRYPTION_KEY` และมี TOTP ciphertext 5 บัญชีที่ใช้ legacy default จึง **ไม่เปลี่ยน key โดยตรง** เพราะจะทำให้ยืนยัน TOTP เดิมไม่ได้

เตรียมไว้แล้ว:

- รองรับ `TWO_FACTOR_PREVIOUS_ENCRYPTION_KEY` เพื่ออ่านข้อมูลเดิมระหว่างเปลี่ยนผ่าน ข้อมูลใหม่เข้ารหัสด้วย primary key และคง raw AES-GCM format เดิม
- ปฏิเสธการสร้าง ciphertext ใหม่เมื่อ primary key ไม่ได้ตั้งค่า เป็น default หรือมีความยาวไม่ถูกต้อง แต่ยังอ่าน legacy ciphertext ได้ระหว่างเปลี่ยนผ่าน ต้อง configure key pair ที่เตรียมไว้ก่อนส่ง traffic ไป slot ใหม่
- `/app/rotate-2fa-key`: default เป็น read-only SQL transaction; `--apply` เป็น atomic migration ที่ต้องสั่งชัดเจน ครอบคลุม users รวม soft-deleted users และ pending TOTP setups; ข้าม email challenges; ข้อมูลเสียแม้หนึ่งแถวจะ rollback ทั้ง transaction; รันซ้ำได้
- `deploy-vm-https/scripts/prepare-2fa-key.py` สร้างไฟล์ key ใหม่แบบ exclusive mode 0600 ใน `.artifacts` ที่ Git ignore โดยไม่แก้ env ของแอปหรือ DB และไม่พิมพ์ค่า key
- Runbook ที่ [verification/README.md](../../deploy-vm-https/verification/README.md) ระบุจังหวะสลับ slot/หยุด old-key writers/ย้าย key/ตรวจ primary-only/เงื่อนไข rollback

ผลทดสอบ: fixture dry-run scanned 1 / needs_rotation 1 / rotated 0; apply rotated 1; primary-only dry-run needs_rotation 0; ใช้ authenticator secret เดิม login ได้ก่อนและหลังย้าย รวมทดสอบ rollback ของ transaction เมื่อพบ ciphertext เสีย

**Production dry-run จริง:** scanned 5 / needs_rotation 5 / rotated 0 ผลเก็บในไฟล์ private `.artifacts/production-2fa-dry-run.json` ไม่มี production ciphertext หรือ key ถูกเปลี่ยน และ application env จริงยังเหมือนเดิม ต้องทำขั้นตอน configure/rotate ตาม runbook พร้อมการ deploy ที่ได้รับอนุญาต

## ผลตรวจสุดท้าย

| Gate | ผล |
|---|---|
| npm audit ทั้งหมด / production | 0 / 0 |
| Frontend security tests | 11/11 รวม dependency compatibility/security |
| TypeScript / production build / Dockerfile build | ผ่าน |
| Go module verify / vet | ผ่าน |
| Go full tests | 257 top-level, รวม subtests 364 passed |
| Go race | handlers, utils, realtime, routes, repositories ผ่าน; รวม regression tests ของ student OAuth และ account linking |
| Docker API/browser | 69/69: baseline 41 + OAuth 18 + 2FA 10 |
| Key rotation | unit/atomic rollback และ actual PostgreSQL transition ผ่าน |
| Load profile | ramp ถึง 50 VUs ใน 30s, 485 HTTP requests, failure rate 0, p95 8.07 ms, thresholds ผ่าน |
| Fixture DB invariant | เช็กชื่อซ้ำมี record เดียว; จบแล้วไม่มี active queue booking ค้าง |
| Source comparison | frontend 456 + backend 234 source/config files ตรงกับสำเนาทดสอบ |

เหลือ skip เดิม 1 subtest ที่ระบุข้อจำกัดยอมรับไว้เรื่อง Mac ปลอม touch hints ไม่ได้นับเป็น passed

Load profile เป็นระบบแยกที่จำกัด CPU/memory บนเครื่องนี้ ผลไม่ใช่การรับรอง capacity production หรือโหลดผู้ใช้หลายพันคน ยังมี warnings เดิมเรื่อง React peer dependency และ deprecated `experimental.useCache` แต่ไม่ใช่ build failures

Images ที่ตรวจ:

- `itii-release-frontend:20261110`: `sha256:3461e18813df230d7623a3da8bb3c61e4e35afb3ee31620aa51fe2fd208c9d75`
- `itii-release-backend:20261110`: `sha256:6d2920cb30d2284c104f9b09bcb2d44c227da75138a3c280060f007464227bf3`

## ขอบเขตที่ต้องยืนยันเมื่อมีระบบจริง

ผู้ใช้ยืนยันว่ายังไม่มี staging URL หรือ test accounts จึงยังไม่ได้ทำ live Google/GitHub/KKU login กับ provider จริง หรือยืนยันเส้นทาง university proxy/Cloudflare จริง การส่ง email/push ไปผู้รับจริงก็ไม่ได้ทำ ผล callback ที่ควบคุมได้และ browser 2FA ผ่านแล้ว ไม่ได้ถูกบันทึกแทนผล live acceptance

ไม่มี production deploy/key rotation ในรอบนี้ บริการเดิมยัง healthy, API health และ login ตอบ 200 และเวลาเริ่ม container เดิมไม่เปลี่ยน

ผล machine-readable: [security-release-verification-2026-11-10.json](security-release-verification-2026-11-10.json)

หลักฐาน: `/tmp/itii-release-20261110` (npm/audit/build), `/tmp/itii-release-backend/final-tests.jsonl`, `/tmp/itii-release-go` (module/vet/race/image logs), `/tmp/itii-release-runtime` (compose, browser/OAuth/MFA/load/rotation reports และ screenshots)

ปิด stack/network ทดสอบและล้าง temporary dependency/build caches แล้ว เก็บ source snapshots, raw test/audit/build logs, screenshots และ verified images ไว้ ผลรอบ image สุดท้ายอยู่ใน `final-runtime.log` (baseline 41 checks) และ `final-auth-oauth.log` (2FA/OAuth 28 checks); แก้ runner mount ที่บัง Playwright dependency แล้วรันสองชุดหลังซ้ำจนผ่านครบ ไม่มี app failure ค้าง

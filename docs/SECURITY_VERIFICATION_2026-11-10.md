# ตรวจงานที่ค้าง — 10 พฤศจิกายน 2026

ผลตรวจซ้ำภายหลังที่ใช้ Docker/Node และ full Go suite ได้แล้ว อยู่ใน [SECURITY_RUNTIME_VERIFICATION_2026-11-10.md](SECURITY_RUNTIME_VERIFICATION_2026-11-10.md) ข้อจำกัดเครื่องมือด้านล่างเป็นบันทึกของรอบก่อนหน้า

สถานะยังไม่พร้อม production: การตรวจรอบนี้ทำได้บางส่วน และไม่ได้ push หรือ deploy

แก้ frontend `services/authenticated-fetch.ts` ให้เก็บ X-CSRF-Token จาก raw response ทุกครั้ง เพื่อให้คำขอแก้ไขข้อมูลครั้งถัดไปส่ง token ล่าสุด สอดคล้องกับ API client หลัก เพิ่ม regression tests 6 กรณีใน `scripts/tests/security-client.test.cjs`: refresh/retry mutation ด้วย CSRF ใหม่, 2FA error ไม่เรียก refresh, data/evidence ใช้ refresh ร่วมกัน, refresh ล้มเหลวไม่ปล่อยคำขอค้าง, หลักฐานที่ไม่มีสิทธิ์ไม่ถูกส่งต่อ และ raw response เก็บ CSRF ใหม่

Tests ฝั่ง frontend เหล่านี้ยังไม่ได้รัน เครื่องนี้ไม่มี Node/npm จึงยังยืนยัน TypeScript/build หรือผลของ tests ใหม่ไม่ได้ ตัว test harness transpile source จริงด้วย TypeScript ที่มีใน devDependencies และจำลอง transport/storage เท่านั้น ไม่ใช่ browser/E2E test

เพิ่ม `.github/workflows/security-verification.yml` ในแต่ละ repository โดยยังไม่ได้ส่งขึ้น GitHub หรือเรียกใช้ workflow:

- Backend: Go ตาม go.mod, download/verify dependencies, tests ทั้งหมด, vet, build API/seed-admin, race tests และ Docker image build
- Frontend: Node 22, npm ci, client regression tests, typecheck, production build และ npm audit สำหรับ production dependencies

ผลที่รันจริงรอบนี้:

- Core packages config/models/repositories/utils/realtime build ผ่าน โดยใช้ temporary modfile/source replacements เดิมใน /tmp ไม่มีการเพิ่ม replace ใน go.mod ของโปรเจกต์
- Utils ผ่าน 17 top-level tests หรือ 25 รายการเมื่อรวม subtests
- Realtime ผ่าน 15 tests จาก source จริง โดยเลือกเฉพาะไฟล์ทดสอบที่ไม่ต้องใช้ miniredis; ยังไม่ใช่ realtime suite ทั้งหมด
- Upload inventory ตรวจ 164 ไฟล์: findings 0, modified_files 0 ไม่ใช่ malware scan
- Go source parse/import/context scan, workflow YAML parse, package/lock dependency maps, bash syntax และ git diff --check ผ่าน

ข้อขัดข้องที่ลองใหม่แล้ว:

- Full go test ./... หยุดที่ setup เนื่องจากโมดูลที่จำเป็นขาดใน cache ซึ่งเป็น read-only และ DNS ภายนอกใช้งานไม่ได้ จึงยังไม่ compile handlers/routes ทั้งชุด
- Frontend verify script หยุดที่ `npm: command not found`
- Go race ไม่มี C compiler
- Docker runtime ตอบ permission denied ที่ socket จึงยังรัน stack/container smoke, browser E2E หรือ load test ไม่ได้

ผลแบบ machine-readable อยู่ใน `security-verification-2026-11-10.json` ผลผ่านของ core ข้างต้นไม่แทนการตรวจ module zip/checksum ตามขั้นตอนปกติ

ขั้นตอนที่ยังต้องทำบนเครื่อง development/CI ที่มีเครื่องมือและ network: รัน `bash scripts/verify-security.sh` ในแต่ละ repository แล้วแก้ failure ที่เกิดขึ้น ก่อนใช้ staging ทดสอบ OAuth/2FA จริง, check-in/queue concurrency, private evidence, session revocation และ proxy/network policy ตามรายการใน SECURITY_HARDENING_2026-11-09.md การสร้าง workflow ในรอบนี้ยังไม่ทำให้ขั้นตอนเหล่านี้มีสถานะผ่าน

# การเชื่อมต่อ KKU Single Sign On (SSONext)

ระบบเข้าสู่ระบบด้วยบัญชีภายนอกของ COCO LABS ใช้ KKU SSO เป็นช่องทางหลักเพียงช่องทางเดียว
ตามที่คณะแจ้ง ช่องทาง Google / GitHub ถูกปิดเป็นค่าเริ่มต้น (เปิดกลับมาได้เฉพาะงานพัฒนา)

เอกสารอ้างอิงจากผู้ให้บริการ: คู่มือการใช้งาน KKU Single Sign On (SSONext)
สำนักเทคโนโลยีดิจิทัล มหาวิทยาลัยขอนแก่น

## ค่าที่ต้องตั้งใน .env ของ backend

| ตัวแปร | ความหมาย |
| --- | --- |
| `KKU_SSO_ENV` | `prod` (ค่าเริ่มต้น) หรือ `uat` เพื่อสลับไปใช้ระบบทดสอบทั้งชุด |
| `KKU_SSO_APP_ID` | App ID ที่ใช้ต่อท้าย `?app=` ของหน้า login/logout |
| `KKU_SSO_CLIENT_ID` | Client ID สำหรับเรียก `auth.token` |
| `KKU_SSO_CLIENT_SECRET` | Client Secret สำหรับเรียก `auth.token` |
| `KKU_SSO_REDIRECT_URL` | Redirect Login URL ที่ลงทะเบียนไว้ ต้องตรงตัวต่อตัว |
| `KKU_SSO_LOGOUT_REDIRECT_URL` | Redirect Logout URL ที่ลงทะเบียนไว้ (ใช้เป็นปลายทางสำรอง) |
| `KKU_SSO_WEB_BASE_URL` | override โดเมนหน้าเว็บ SSO (ปกติไม่ต้องตั้ง) |
| `KKU_SSO_API_BASE_URL` | override โดเมน REST API ของ SSO (ปกติไม่ต้องตั้ง) |

ค่าที่ใช้อยู่ตอนนี้ (ตั้งไว้แล้วใน `.env` และ `.env.backend` ซึ่งไม่ถูก commit)

- Redirect Login URL: `https://cocolabs.computing.kku.ac.th/api/auth/kku/callback`
- Redirect Logout URL: `https://cocolabs.computing.kku.ac.th/logout`

ค่าเริ่มต้นของโดเมนตาม `KKU_SSO_ENV`

| ENV | หน้าเว็บ | REST API |
| --- | --- | --- |
| `prod` | `https://ssonext.kku.ac.th` | `https://ssonext-api.kku.ac.th` |
| `uat` | `https://sso-uat-web.kku.ac.th` | `https://sso-uat-api.kku.ac.th` |

> คู่มือที่ได้รับเป็นฉบับ UAT ถ้าสำนักเทคโนโลยีดิจิทัลยืนยันว่าชุดข้อมูลที่ออกให้
> ใช้ได้เฉพาะระบบทดสอบ ให้ตั้ง `KKU_SSO_ENV=uat` แล้ว deploy ใหม่ ไม่ต้องแก้โค้ด

## Endpoint ฝั่งเรา

| เส้นทาง | หน้าที่ |
| --- | --- |
| `GET /api/auth/kku` | เริ่ม flow พาไปหน้า login ของ SSONext รองรับ `?audience=student` และ `?action=link` |
| `GET /api/auth/kku/callback` | Redirect Login URL แลก code เป็น token แล้วออกเซสชันของระบบ |
| `GET /api/auth/kku/logout` | เพิกถอนเซสชันฝั่งเรา แล้วส่งต่อไปหน้า logout ของ SSONext |
| `GET /api/auth/kku/config` | บอกว่าเปิดใช้งาน SSO อยู่หรือไม่ (ไม่เปิดเผยความลับ) |
| `GET /logout` (frontend) | Redirect Logout URL หน้าปลายทางหลังปิดเซสชันกลาง |

## ลำดับการทำงาน

1. ผู้ใช้กดปุ่ม KKU SSO หน้าเว็บพาไป `/api/auth/kku`
2. backend ออกคุกกี้ `kku_sso_nonce` (HMAC-SHA256 ด้วย `JWT_SECRET`, อายุ 5 นาที)
   เก็บ audience และ action ไว้ เพราะ SSONext ไม่มีพารามิเตอร์ `state` ให้ฝากค่า
   จากนั้น redirect ไป `<web>/login?app=<AppID>`
3. SSO ส่งกลับมาที่ `/api/auth/kku/callback?code=...`
4. backend ตรวจ nonce แล้วเรียก `POST <api>/auth.token`
   ด้วย `{ code, redirectUrl, clientId, clientSecret }`
5. เรียก `POST <api>/user.profile` ต่อ เพื่อได้ชื่อไทย/อังกฤษ คณะ ตำแหน่ง และ `userId`
   ขั้นนี้ไม่บังคับ ถ้าเรียกไม่สำเร็จยังล็อกอินต่อได้ด้วยข้อมูลจาก `auth.token`
6. จับคู่ผู้ใช้ด้วย `provider_user_id` (ลำดับ `immutableId` → `userId` → `citizenId` →
   `email:<email>`) ถ้าไม่พบจึงค่อยจับคู่ด้วยอีเมล
7. ออก access/refresh token ของระบบ ตั้งเป็นคุกกี้ httpOnly แล้ว redirect ไป
   `/auth/callback?login=success` (ถ้าเปิด 2FA จะส่งไปหน้ายืนยันตัวตนก่อน)

การออกจากระบบ: `POST /api/auth/logout` จะคืน `data.ssoLogoutUrl` เมื่อเซสชันนั้นมาจาก
KKU SSO หน้าเว็บพาเบราว์เซอร์ไป URL นั้นเพื่อปิดเซสชันกลาง ไม่งั้นกดเข้าสู่ระบบอีกครั้ง
จะเด้งกลับเข้ามาทันทีโดยไม่ถามรหัสผ่าน

## ข้อควรรู้

- **ไม่มีการสร้างบัญชีอัตโนมัติ** อาจารย์/ทีเอ/แอดมิน ต้องมีบัญชีในตาราง `users` อยู่ก่อน
  ส่วนนักศึกษาต้องมีอีเมลตรงกับข้อมูลในตาราง `students` และสถานะยังใช้งานได้
- `citizenId` (เลขบัตรประชาชน) ไม่ถูกใช้เป็นตัวระบุหลักและไม่ถูกบันทึกลง system log
  แถวเก่าที่เคยผูกด้วย `citizenId` จะถูกย้ายมาเป็น `immutableId` ให้อัตโนมัติเมื่อล็อกอินครั้งถัดไป
- `redirectUrl` ที่ส่งไปกับ `auth.token` ยึดค่าจาก `KKU_SSO_REDIRECT_URL` เป็นหลัก
  ไม่เดาจาก host ของ request เพราะฝั่ง SSO เทียบค่านี้แบบตัวต่อตัว
- Client Secret อยู่ใน `.env` / `.env.backend` เท่านั้น ทั้งสองไฟล์อยู่ใน `.gitignore`

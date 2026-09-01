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
| `KKU_SSO_SINGLE_LOGOUT` | `false` (ค่าเริ่มต้น) ออกจากระบบเฉพาะเว็บนี้ ตั้ง `true` เมื่อต้องการปิดเซสชันกลางด้วย |
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
| `GET /api/auth/kku/logout` | ออกจากระบบแบบ single logout ปุ่มปกติของเว็บไม่เรียกเส้นทางนี้ |
| `GET /api/auth/kku/config` | บอกว่าเปิดใช้งาน SSO อยู่หรือไม่ (ไม่เปิดเผยความลับ) |
| `GET /logout` (frontend) | Redirect Logout URL หน้าปลายทางหลังปิดเซสชันกลาง |

## สองโดเมน สองช่องทางล็อกอิน

ระบบเปิดสองประตูเข้าเว็บ

| โดเมน | เส้นทาง | ช่องทางล็อกอินหลัก |
| --- | --- | --- |
| `cocolabs.computing.kku.ac.th` | reverse proxy ของ มข. | KKU SSO (SSONext) |
| `cocolab.osp101.com` | Cloudflare Tunnel (โดเมนสำรอง) | Google |

เหตุผล: Redirect Login URL ที่ลงทะเบียนกับสำนักเทคโนโลยีดิจิทัลผูกกับโดเมนหลัก
ตัวต่อตัว ถ้าเริ่ม flow จากโดเมนสำรอง ปลายทาง callback จะวิ่งกลับไปที่โดเมนหลัก
ซึ่งตอนนั้นมักล่มอยู่พอดี (ซึ่งคือเหตุผลที่ผู้ใช้ต้องมาโดเมนสำรองตั้งแต่แรก)
โดเมนสำรองจึงใช้ Google ที่ผูก callback ตามโดเมนของ request ได้

การตัดสินใจอยู่ฝั่งหน้าเว็บที่ `lib/auth-providers.ts` `resolveLoginProviderMode()`
อ่านจาก `window.location.hostname` ตอน runtime ไม่ใช่ตอน build เพราะ build ชุดเดียว
ถูกเสิร์ฟทั้งสองโดเมน กฎคือ hostname ที่ลงท้ายด้วย `kku.ac.th` ใช้ KKU SSO
นอกนั้นใช้ Google บังคับค่าได้ด้วย `NEXT_PUBLIC_LOGIN_PROVIDER_MODE=kku|google`
(ใช้ตอนพัฒนาในเครื่อง)

ฝั่ง backend มีด่านซ้ำอีกชั้น ถ้ามีใครยิง `GET /api/auth/kku` จากโดเมนที่ไม่ตรงกับ
`KKU_SSO_REDIRECT_URL` จะตอบ 503 พร้อม `code: KKU_SSO_WRONG_DOMAIN` แทนที่จะ
ปล่อยให้ไปเจอหน้า error ของ SSO และ `GET /api/auth/kku/config` มีฟิลด์
`domainSupported` บอกสถานะเดียวกัน

หน้าโปรไฟล์ก็ใช้กฎเดียวกัน ผูกบัญชี KKU ได้เฉพาะโดเมนหลัก ผูกบัญชี Google ได้
เฉพาะโดเมนสำรอง ส่วนบัญชีที่ผูกไว้แล้วยังยกเลิกการเชื่อมต่อได้จากทุกโดเมน

> ต้องเพิ่ม `https://cocolab.osp101.com/api/auth/google/callback` เข้าไปใน
> Authorized redirect URIs ของ Google Cloud Console ไม่งั้นล็อกอินบนโดเมนสำรอง
> จะติด `redirect_uri_mismatch`

### ชั่วคราว: ปุ่ม Google บนโดเมนหลัก (เพิ่ม 1 ก.ย. 2569)

ระหว่างที่ข้อมูลผู้ใช้ในระบบ SSO ของสำนักยังไม่ครบ หน้าล็อกอินบนโดเมนหลักมีกล่อง
สีเหลืองพร้อมปุ่ม Google เป็นช่องทางสำรองเพิ่มมาให้ และหน้าโปรไฟล์ก็ผูกบัญชี Google
บนโดเมนหลักได้ด้วย

ปิดชั่วคราวโดยไม่แก้โค้ด: `NEXT_PUBLIC_TEMP_GOOGLE_FALLBACK=false` ตอน build

วิธีเอาออกถาวรเมื่อสำนักอัปเดตข้อมูลครบแล้ว ค้นคำว่า
`TEMP_GOOGLE_FALLBACK_ON_KKU_DOMAIN` แล้วลบทั้งหมด 4 จุด

- `lib/auth-providers.ts` (ตัวค่าคงที่)
- `app/login/page.tsx` (กล่องสีเหลือง)
- `app/student/login/page.tsx` (กล่องสีเหลือง)
- `components/profile/AuthenticationSection.tsx` (เงื่อนไขใน isProviderConnectable)

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

## การออกจากระบบ

ค่าเริ่มต้นคือออกจากระบบเฉพาะเว็บนี้ `POST /api/auth/logout` จะเพิกถอน refresh token
และล้างคุกกี้ โดยไม่แตะเซสชันกลางของมหาวิทยาลัย เหตุผลคือการปิดเซสชันกลางเท่ากับ
เตะผู้ใช้ออกจากทุกบริการที่ใช้ KKU SSO ร่วมกัน ทั้งที่เขาตั้งใจออกจากระบบนี้ระบบเดียว

ผลข้างเคียงที่ต้องรู้: หลังออกจากระบบแล้วกดปุ่มเข้าสู่ระบบใหม่ ผู้ใช้จะกลับเข้ามาได้ทันที
เพราะเซสชันกลางยังอยู่ ระบบจะไม่ถามรหัสผ่านซ้ำ ถ้าใช้เครื่องคอมพิวเตอร์ส่วนกลาง
ให้ผู้ใช้เข้า `GET /api/auth/kku/logout` เพื่อออกจากทุกบริการพร้อมกัน หรือปิดเบราว์เซอร์

ถ้าต้องการให้ปุ่มออกจากระบบทำ single logout ทุกครั้ง ตั้ง `KKU_SSO_SINGLE_LOGOUT=true`
แล้ว `POST /api/auth/logout` จะคืน `data.ssoLogoutUrl` มาให้หน้าเว็บพาเบราว์เซอร์ไปต่อ

## ข้อควรรู้

- **ไม่มีการสร้างบัญชีอัตโนมัติ** อาจารย์/ทีเอ/แอดมิน ต้องมีบัญชีในตาราง `users` อยู่ก่อน
  ส่วนนักศึกษาต้องมีอีเมลตรงกับข้อมูลในตาราง `students` และสถานะยังใช้งานได้
- `citizenId` (เลขบัตรประชาชน) ไม่ถูกใช้เป็นตัวระบุหลักและไม่ถูกบันทึกลง system log
  แถวเก่าที่เคยผูกด้วย `citizenId` จะถูกย้ายมาเป็น `immutableId` ให้อัตโนมัติเมื่อล็อกอินครั้งถัดไป
- `redirectUrl` ที่ส่งไปกับ `auth.token` ยึดค่าจาก `KKU_SSO_REDIRECT_URL` เป็นหลัก
  ไม่เดาจาก host ของ request เพราะฝั่ง SSO เทียบค่านี้แบบตัวต่อตัว
- Client Secret อยู่ใน `.env` / `.env.backend` เท่านั้น ทั้งสองไฟล์อยู่ใน `.gitignore`

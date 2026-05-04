# ITII Assist Classroom — สรุปฟังก์ชันของระบบ (Feature Summary)

> เอกสารนี้สรุปฟังก์ชันทั้งหมดของเว็บแอปพลิเคชัน ITII Assist Classroom
> ซึ่งเป็นระบบสนับสนุนการเรียนการสอนสำหรับคณาจารย์และผู้ช่วยสอน (TA)

---

## สารบัญ

1. [ระบบยืนยันตัวตนและความปลอดภัย (Authentication & Security)](#1-ระบบยืนยันตัวตนและความปลอดภัย)
2. [การยืนยันตัวตนสองชั้น (Two-Factor Authentication)](#2-การยืนยันตัวตนสองชั้น)
3. [การจัดการผู้ใช้ (User Management)](#3-การจัดการผู้ใช้)
4. [การจัดการนักศึกษา (Student Management)](#4-การจัดการนักศึกษา)
5. [การจัดการรายวิชา (Course Management)](#5-การจัดการรายวิชา)
6. [การจัดการห้องเรียน (Classroom Management)](#6-การจัดการห้องเรียน)
7. [แดชบอร์ดอาจารย์/TA (Instructor Dashboard)](#7-แดชบอร์ดอาจารย์ta)
8. [การจัดการงานมอบหมาย (Assignment Management)](#8-การจัดการงานมอบหมาย)
9. [ระบบให้คะแนน (Scoring System)](#9-ระบบให้คะแนน)
10. [ระบบขอแก้ไขคะแนน (Score Edit Request)](#10-ระบบขอแก้ไขคะแนน)
11. [คะแนนโบนัส (Bonus Scores)](#11-คะแนนโบนัส)
12. [คะแนนสอบ (Exam Scores)](#12-คะแนนสอบ)
13. [ระบบเช็คชื่อ (Attendance System)](#13-ระบบเช็คชื่อ)
14. [ระบบจองคิว/ตรวจงาน (Queue System)](#14-ระบบจองคิวตรวจงาน)
15. [การจัดการทีม/กลุ่ม (Team/Group Management)](#15-การจัดการทีมกลุ่ม)
16. [ระบบ Feedback (Feedback System)](#16-ระบบ-feedback)
17. [การแจ้งเตือน Push Notification](#17-การแจ้งเตือน-push-notification)
18. [บันทึกกิจกรรมรายวิชา (Course Activity Logs)](#18-บันทึกกิจกรรมรายวิชา)
19. [การบริหารระบบ (System Administration)](#19-การบริหารระบบ)
20. [การสื่อสาร Real-time (Real-time Communication)](#20-การสื่อสาร-real-time)
21. [โครงสร้างพื้นฐาน DevOps](#21-โครงสร้างพื้นฐาน-devops)

---

## 1. ระบบยืนยันตัวตนและความปลอดภัย

### คำอธิบาย
ระบบยืนยันตัวตนรองรับการเข้าสู่ระบบด้วยชื่อผู้ใช้/รหัสผ่าน และ OAuth ผ่าน Social Login พร้อมระบบจัดการ Session ด้วย JWT

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| Login ด้วย Username/Password | เข้าสู่ระบบด้วยชื่อผู้ใช้และรหัสผ่าน ได้ JWT access token + refresh token |
| OAuth Social Login | เข้าสู่ระบบผ่าน Google, GitHub, Apple |
| JWT Refresh Token | ต่ออายุ access token อัตโนมัติ โดยไม่ต้องล็อกอินใหม่ |
| Session Management | ดูรายการ session ที่ active, ยกเลิก session เฉพาะ หรือทั้งหมด |
| เปลี่ยนรหัสผ่าน | เปลี่ยนรหัสผ่านขณะล็อกอินอยู่ |
| บังคับเปลี่ยนรหัสผ่าน | บังคับให้เปลี่ยนรหัสผ่านเมื่อเข้าสู่ระบบครั้งแรก |
| ลืมรหัสผ่าน | ส่งลิงก์ reset ไปยังอีเมลเพื่อตั้งรหัสผ่านใหม่ |
| จัดการโปรไฟล์ | แก้ไขข้อมูลส่วนตัว, อัปโหลด/ลบรูปโปรไฟล์ |
| เชื่อมต่อบัญชี OAuth | เชื่อม/ยกเลิกบัญชี Google, GitHub, Apple กับบัญชีผู้ใช้ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| POST | `/api/auth/login` | เข้าสู่ระบบ |
| POST | `/api/auth/refresh` | ต่ออายุ token |
| POST | `/api/auth/logout` | ออกจากระบบ |
| GET | `/api/auth/me` | ดูข้อมูลผู้ใช้ปัจจุบัน |
| POST | `/api/auth/change-password` | เปลี่ยนรหัสผ่าน |
| POST | `/api/auth/force-change-password` | บังคับเปลี่ยนรหัสผ่าน |
| PUT | `/api/auth/profile` | แก้ไขโปรไฟล์ |
| GET | `/api/auth/sessions` | ดู session ที่ active |
| DELETE | `/api/auth/sessions/:id` | ยกเลิก session |
| POST | `/api/auth/sessions/revoke-all` | ยกเลิกทุก session |
| POST | `/api/auth/avatar` | อัปโหลดรูปโปรไฟล์ |
| DELETE | `/api/auth/avatar` | ลบรูปโปรไฟล์ |
| GET | `/api/auth/google` | เข้าสู่ระบบผ่าน Google |
| GET | `/api/auth/google/callback` | Google OAuth callback |
| GET | `/api/auth/github` | เข้าสู่ระบบผ่าน GitHub |
| GET | `/api/auth/github/callback` | GitHub OAuth callback |
| POST | `/api/auth/apple` | เข้าสู่ระบบผ่าน Apple |
| POST | `/api/auth/apple/callback` | Apple OAuth callback |
| POST | `/api/auth/forgot-password` | ขอรีเซ็ตรหัสผ่าน |
| POST | `/api/auth/validate-reset-token` | ตรวจสอบ token รีเซ็ต |
| POST | `/api/auth/reset-password` | รีเซ็ตรหัสผ่าน |
| GET | `/api/oauth/linked` | ดูบัญชี OAuth ที่เชื่อม |
| POST | `/api/oauth/link` | เชื่อมบัญชี OAuth |
| DELETE | `/api/oauth/unlink/:provider` | ยกเลิกเชื่อม OAuth |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/login` | หน้าเข้าสู่ระบบ |
| `/auth/callback` | หน้ารับ OAuth callback |
| `/auth/link-callback` | หน้าเชื่อม OAuth กับบัญชี |
| `/auth/reset-password` | หน้ารีเซ็ตรหัสผ่าน |
| `/profile` | หน้าจัดการโปรไฟล์ |

---

## 2. การยืนยันตัวตนสองชั้น

### คำอธิบาย
ระบบ 2FA รองรับทั้ง TOTP (แอป Authenticator) และรหัสผ่านทางอีเมล พร้อม backup codes

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ตั้งค่า TOTP | ตั้งค่า 2FA ผ่านแอป Authenticator (Google Authenticator, Authy ฯลฯ) |
| ตั้งค่า Email 2FA | ตั้งค่า 2FA ผ่านอีเมล |
| ยืนยันและเปิดใช้งาน | ยืนยันรหัส OTP เพื่อเปิดใช้ 2FA |
| ปิดใช้งาน 2FA | ปิดการยืนยันสองชั้น |
| Backup Codes | สร้าง backup codes สำหรับกรณีฉุกเฉิน |
| ตรวจสอบตอนล็อกอิน | ถ้าเปิด 2FA ต้องยืนยันรหัสก่อนเข้าระบบ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/auth/2fa/status` | ดูสถานะ 2FA |
| POST | `/api/auth/2fa/setup/totp` | เริ่มตั้งค่า TOTP |
| POST | `/api/auth/2fa/setup/email` | เริ่มตั้งค่า Email 2FA |
| POST | `/api/auth/2fa/verify` | ยืนยันรหัสเปิดใช้ 2FA |
| POST | `/api/auth/2fa/resend-email` | ส่งรหัสอีเมลซ้ำ |
| POST | `/api/auth/2fa/disable` | ปิดใช้งาน 2FA |
| POST | `/api/auth/2fa/backup-codes` | สร้าง backup codes ใหม่ |
| POST | `/api/auth/2fa/verify-login` | ยืนยัน 2FA ตอนล็อกอิน |
| POST | `/api/auth/2fa/send-login-code` | ส่งรหัส 2FA ทางอีเมล |
| POST | `/api/auth/2fa/complete-login` | ล็อกอินเสร็จหลังยืนยัน 2FA |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/auth/verify-2fa` | หน้ายืนยัน 2FA ตอนล็อกอิน |

---

## 3. การจัดการผู้ใช้

### คำอธิบาย
ผู้ดูแลระบบ (Admin) จัดการบัญชีผู้ใช้งานทั้งหมดในระบบ รวมถึงอาจารย์และ TA

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ดูรายชื่อผู้ใช้ | แสดงรายชื่อผู้ใช้ทั้งหมด พร้อม pagination และ filter |
| สร้างผู้ใช้ใหม่ | สร้างบัญชีผู้ใช้ใหม่ กำหนด role (admin/instructor/ta) |
| แก้ไขผู้ใช้ | แก้ไขข้อมูลผู้ใช้ |
| ลบผู้ใช้ | ลบบัญชีผู้ใช้ |
| เปิด/ปิดสถานะ | Toggle สถานะ active/inactive |
| สถิติผู้ใช้ | แสดงจำนวนผู้ใช้แบ่งตาม role |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/users` | ดูรายชื่อผู้ใช้ (pagination + filter) |
| GET | `/api/users/stats` | สถิติผู้ใช้ |
| GET | `/api/users/:id` | ดูผู้ใช้รายบุคคล |
| POST | `/api/users` | สร้างผู้ใช้ |
| PUT | `/api/users/:id` | แก้ไขผู้ใช้ |
| DELETE | `/api/users/:id` | ลบผู้ใช้ |
| PATCH | `/api/users/:id/status` | Toggle สถานะ |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/admin/users` | หน้าจัดการผู้ใช้ |

---

## 4. การจัดการนักศึกษา

### คำอธิบาย
จัดการข้อมูลนักศึกษาในระบบ รองรับการ Import จากไฟล์ CSV/Excel

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ดูรายชื่อนักศึกษา | แสดงรายชื่อทั้งหมด พร้อม pagination และ filter |
| สร้างนักศึกษา | เพิ่มนักศึกษาใหม่ |
| Import นักศึกษา | นำเข้าข้อมูลจากไฟล์ CSV/Excel |
| ค้นหานักศึกษา | ค้นหาตามรหัสนักศึกษา (รองรับ bulk search) |
| ตรวจสอบคะแนน | นักศึกษาดูคะแนนตัวเองผ่านหน้า My Score |
| เปิด/ปิดสถานะ | Toggle สถานะ active/inactive |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/students` | ดูรายชื่อนักศึกษา |
| GET | `/api/students/stats` | สถิติ |
| GET | `/api/students/:id` | ดูรายบุคคล |
| POST | `/api/students` | สร้างนักศึกษา |
| POST | `/api/students/import` | Import จากไฟล์ |
| PUT | `/api/students/:id` | แก้ไข |
| DELETE | `/api/students/:id` | ลบ |
| PATCH | `/api/students/:id/status` | Toggle สถานะ |
| GET | `/api/students/lookup/:student_id` | ค้นหาคะแนนรายบุคคล (public) |
| POST | `/api/students/search-by-ids` | ค้นหาหลายรหัส (bulk) |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/admin/students` | หน้าจัดการนักศึกษา |
| `/myscore` | หน้าตรวจสอบคะแนนตัวเอง |

---

## 5. การจัดการรายวิชา

### คำอธิบาย
ระบบจัดการรายวิชา รวมถึง Section, การกำหนดอาจารย์/TA และการลงทะเบียนนักศึกษา

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| CRUD รายวิชา | สร้าง ดู แก้ไข ลบรายวิชา พร้อมรายละเอียด ปี/เทอม |
| จัดการ Section | เพิ่ม แก้ไข ลบ Section ภายในรายวิชา |
| กำหนด TA | เพิ่ม/ลบ TA (รองรับ bulk) |
| กำหนดอาจารย์ | เพิ่ม/ลบ อาจารย์เพิ่มเติม (รองรับ bulk) |
| ลงทะเบียนนักศึกษา | เพิ่ม/ลบ นักศึกษาในแต่ละ Section (รองรับ bulk) |
| เปิด/ปิดรายวิชา | Toggle สถานะ active/inactive |
| Course Overview | ดูภาพรวมของรายวิชา |
| My Courses | ดูรายวิชาที่อาจารย์/TA รับผิดชอบ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/courses` | ดูรายวิชาทั้งหมด |
| GET | `/api/courses/my-courses` | ดูรายวิชาของตนเอง |
| GET | `/api/courses/my-courses/stats` | สถิติรายวิชาของตนเอง |
| GET | `/api/courses/stats` | สถิติรายวิชาทั้งหมด |
| GET | `/api/courses/instructors` | รายชื่ออาจารย์ (dropdown) |
| GET | `/api/courses/tas-list` | รายชื่อ TA (dropdown) |
| GET | `/api/courses/:id` | ดูรายวิชาเดียว |
| GET | `/api/courses/:id/overview` | ดูภาพรวมรายวิชา |
| POST | `/api/courses` | สร้างรายวิชา |
| PUT | `/api/courses/:id` | แก้ไขรายวิชา |
| DELETE | `/api/courses/:id` | ลบรายวิชา |
| PATCH | `/api/courses/:id/toggle-status` | Toggle สถานะ |
| POST | `/api/courses/:id/sections` | เพิ่ม Section |
| PUT | `/api/courses/:id/sections/:sectionId` | แก้ไข Section |
| DELETE | `/api/courses/:id/sections/:sectionId` | ลบ Section |
| POST | `/api/courses/:id/tas` | เพิ่ม TA |
| POST | `/api/courses/:id/tas/bulk` | เพิ่ม TA หลายคน |
| DELETE | `/api/courses/:id/tas/:userId` | ลบ TA |
| POST | `/api/courses/:id/instructors` | เพิ่มอาจารย์ |
| POST | `/api/courses/:id/instructors/bulk` | เพิ่มอาจารย์หลายคน |
| DELETE | `/api/courses/:id/instructors/:userId` | ลบอาจารย์ |
| GET | `/api/courses/:id/sections/:sectionId/students` | ดูนักศึกษาใน Section |
| POST | `/api/courses/:id/sections/:sectionId/students` | เพิ่มนักศึกษา |
| POST | `/api/courses/:id/sections/:sectionId/students/bulk` | เพิ่มนักศึกษา bulk |
| DELETE | `/api/courses/:id/sections/:sectionId/students/:studentId` | ลบนักศึกษา |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/admin/courses` | หน้าจัดการรายวิชา (admin) |
| `/(instructor)/home` | หน้ารายวิชาที่สอน/เป็น TA |
| `/(instructor)/home/closed` | หน้ารายวิชาที่ปิดแล้ว |

---

## 6. การจัดการห้องเรียน

### คำอธิบาย
จัดการห้องเรียนจริง พร้อม Canvas Editor สำหรับออกแบบ Layout ตำแหน่งโต๊ะและโซน

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| CRUD ห้องเรียน | สร้าง ดู แก้ไข ลบห้องเรียน (ชื่อ, อาคาร, ชั้น) |
| Canvas Editor | ตัวแก้ไขแบบ interactive สำหรับวางตำแหน่งโต๊ะ |
| จัดการโต๊ะ | เพิ่ม/ลบ/ย้ายโต๊ะ กำหนดประเภท (computer/normal/teacher) |
| จัดการโซน | สร้างโซนพื้นที่ภายในห้อง (เช่น โซน A, แถวหน้า) |
| Soft Delete | ลบห้องเรียนแบบซ่อน สามารถกู้คืนได้ |
| เปิด/ปิดสถานะ | Toggle สถานะ active/inactive |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/classrooms` | ดูทั้งหมด |
| GET | `/api/classrooms/stats` | สถิติ |
| GET | `/api/classrooms/:id` | ดูรายละเอียดพร้อม layout |
| POST | `/api/classrooms` | สร้าง |
| PUT | `/api/classrooms/:id` | แก้ไข |
| PUT | `/api/classrooms/:id/layout` | บันทึก layout (ตำแหน่งโต๊ะ) |
| PATCH | `/api/classrooms/:id/toggle-status` | Toggle สถานะ |
| POST | `/api/classrooms/:id/restore` | กู้คืนห้องที่ลบ |
| DELETE | `/api/classrooms/:id` | ลบ |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/admin/classrooms` | หน้าจัดการห้องเรียน + Canvas Editor |

---

## 7. แดชบอร์ดอาจารย์/TA

### คำอธิบาย
หน้าจัดการรายวิชารายตัว สำหรับอาจารย์และ TA มี Tab ต่างๆ สำหรับจัดการทุกด้านของรายวิชา

### Tab ที่มี

| Tab | คำอธิบาย |
|---|---|
| **Overview** | ภาพรวมรายวิชา — จำนวนนักศึกษา, สถิติ, กิจกรรมล่าสุด |
| **Sections** | จัดการ Section และนักศึกษาที่ลงทะเบียน |
| **Assignments** | สร้าง/แก้ไข/จัดลำดับงานมอบหมาย พร้อม sub-items และหมวดหมู่ |
| **Attendance** | สร้าง/เปิด/ปิดรอบเช็คชื่อ ดูผลการเช็คชื่อ |
| **Queue** | จัดการคิวตรวจงาน (สร้าง/เปิด/ปิด session) |
| **Score Summary** | ตารางคะแนนรวม (นักศึกษา × งานมอบหมาย) |
| **Score Approval** | ตรวจสอบและอนุมัติ/ปฏิเสธคำขอแก้ไขคะแนนจาก TA |
| **Exam Scores** | จัดการคะแนนสอบกลางภาค/ปลายภาค |
| **People** | ดูรายชื่ออาจารย์, TA และนักศึกษาในรายวิชา |
| **TA Stats** | สถิติการทำงานของ TA |
| **Activity Log** | บันทึกกิจกรรมทุกอย่างที่เกิดขึ้นในรายวิชา (audit trail) |
| **Settings** | ตั้งค่ารายวิชา (เกณฑ์เวลาสาย, เกณฑ์ความสนใจ, การแสดงคะแนน) |

### หน้าเสริม

| เส้นทาง | คำอธิบาย |
|---|---|
| `/(instructor)/classroom/[id]` | หน้าหลัก dashboard รายวิชา |
| `/(instructor)/classroom/[id]/attendance/[sessionId]/summary` | สรุปผลเช็คชื่อรายรอบ |
| `/(instructor)/classroom/[id]/queue/[sessionId]/worker` | หน้าตรวจงานสำหรับ TA (worker interface) |

---

## 8. การจัดการงานมอบหมาย

### คำอธิบาย
สร้างและจัดการงานมอบหมาย (Assignment) ภายในรายวิชา รองรับ sub-items, หมวดหมู่ และการเชื่อมโยงกับการเช็คชื่อ

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| CRUD งานมอบหมาย | สร้าง ดู แก้ไข ลบ Assignment |
| ประเภทงาน | individual (เดี่ยว), permanent_group (กลุ่มถาวร), weekly_group (กลุ่มรายสัปดาห์), assignment (การบ้าน) |
| Sub-items | เพิ่มหัวข้อย่อยพร้อมคะแนนเต็มแต่ละข้อ |
| การจัดลำดับ | Drag & drop จัดลำดับงาน |
| เชื่อมกับเช็คชื่อ | เชื่อมงานกับรอบเช็คชื่อ (เงื่อนไข AND/OR) |
| การแสดงคะแนน | กำหนดว่าจะให้นักศึกษาเห็นคะแนนงานนี้หรือไม่ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/assignments` | ดูทั้งหมดในรายวิชา |
| GET | `/api/assignments/:id` | ดูรายละเอียด |
| POST | `/api/assignments` | สร้าง |
| PUT | `/api/assignments/:id` | แก้ไข |
| DELETE | `/api/assignments/:id` | ลบ |
| PUT | `/api/assignments/reorder/batch` | จัดลำดับใหม่ |

---

## 9. ระบบให้คะแนน

### คำอธิบาย
ระบบให้คะแนนนักศึกษา รองรับทั้งรายบุคคล, รายกลุ่ม และ bulk พร้อม Score Matrix

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ให้คะแนนรายบุคคล | ให้คะแนนนักศึกษาทีละคน |
| ให้คะแนนกลุ่ม | ให้คะแนนเดียวกันทั้งกลุ่ม |
| ให้คะแนน Bulk | ให้คะแนนหลายคนพร้อมกัน |
| Score Matrix | ตารางคะแนนรวม (นักศึกษา × งาน) |
| Score Summary | สรุปคะแนนนักศึกษาแต่ละคน |
| ค้นหานักศึกษา | Autocomplete ขณะพิมพ์ชื่อ/รหัส |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/scores` | ดูคะแนนตามงาน |
| GET | `/api/scores/summary` | สรุปคะแนน |
| GET | `/api/scores/matrix` | Score matrix |
| GET | `/api/scores/students/search` | ค้นหานักศึกษา |
| GET | `/api/scores/groups` | ดูกลุ่มสำหรับให้คะแนน |
| POST | `/api/scores` | ให้คะแนนรายบุคคล |
| POST | `/api/scores/bulk` | ให้คะแนน bulk |
| POST | `/api/scores/group` | ให้คะแนนกลุ่ม |

---

## 10. ระบบขอแก้ไขคะแนน

### คำอธิบาย
TA สามารถส่งคำขอแก้ไขคะแนน ไปยังอาจารย์เพื่ออนุมัติ/ปฏิเสธ รองรับแนบรูปภาพเพื่อประกอบเหตุผล

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ส่งคำขอ | TA ส่งคำขอแก้ไขคะแนนพร้อมเหตุผลและรูปภาพ |
| ส่งคำขอ Batch | ส่งคำขอแก้ไขสำหรับทั้งกลุ่ม |
| อนุมัติ/ปฏิเสธ | อาจารย์อนุมัติหรือปฏิเสธ พร้อม comment |
| Batch Approve/Reject | อนุมัติ/ปฏิเสธหลายรายการพร้อมกัน |
| จำนวนคำขอรอดำเนินการ | แสดง Badge จำนวนคำขอที่รออนุมัติ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/score-edit-requests` | ดูคำขอแก้ไขในรายวิชา |
| GET | `/api/score-edit-requests/pending-count` | จำนวนที่รออนุมัติ |
| POST | `/api/score-edit-requests` | สร้างคำขอ (แนบรูปได้) |
| POST | `/api/score-edit-requests/batch` | สร้างคำขอ batch |
| POST | `/api/score-edit-requests/batch-approve` | อนุมัติ batch |
| POST | `/api/score-edit-requests/batch-reject` | ปฏิเสธ batch |
| POST | `/api/score-edit-requests/:id/approve` | อนุมัติรายตัว |
| POST | `/api/score-edit-requests/:id/reject` | ปฏิเสธรายตัว |

---

## 11. คะแนนโบนัส

### คำอธิบาย
ระบบให้คะแนนพิเศษ (Bonus) สำหรับนักศึกษาที่มีส่วนร่วมในชั้นเรียน เช่น ตอบคำถาม

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| เพิ่มคะแนนโบนัส | ให้คะแนนพิเศษพร้อมเหตุผล |
| ดูคะแนนโบนัส | ดูรายการและสรุปคะแนนโบนัสทั้งรายวิชา |
| ประวัติรายบุคคล | ดูประวัติคะแนนโบนัสของนักศึกษาแต่ละคน |
| ลบคะแนนโบนัส | ลบรายการที่ผิดพลาด |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| POST | `/api/bonus-scores` | เพิ่มคะแนนโบนัส |
| GET | `/api/bonus-scores/course/:courseId/students` | ดูนักศึกษาสำหรับเลือก |
| GET | `/api/bonus-scores/course/:courseId` | ดูคะแนนโบนัสทั้งรายวิชา |
| GET | `/api/bonus-scores/course/:courseId/summary` | สรุปคะแนนโบนัส |
| GET | `/api/bonus-scores/course/:courseId/student/:studentId` | ประวัติรายบุคคล |
| DELETE | `/api/bonus-scores/:id` | ลบ |

---

## 12. คะแนนสอบ

### คำอธิบาย
จัดการคะแนนสอบกลางภาค/ปลายภาค แยกตาม component (Lab/Lecture) พร้อมน้ำหนักคะแนน

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ตั้งค่าการสอบ | กำหนดประเภทสอบ, คะแนนเต็ม, น้ำหนัก |
| บันทึกคะแนนสอบ | บันทึกรายบุคคลหรือ bulk |
| สถิติคะแนนสอบ | ดูค่าเฉลี่ย, สูงสุด, ต่ำสุด, การกระจาย |
| การมองเห็น | กำหนดว่านักศึกษาเห็นคะแนนได้หรือไม่ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/courses/:courseId/exam-settings` | ดูตั้งค่าการสอบ |
| PUT | `/api/courses/:courseId/exam-settings/:settingId` | แก้ไขตั้งค่า |
| GET | `/api/courses/:courseId/exam-scores` | ดูคะแนนสอบ |
| GET | `/api/courses/:courseId/exam-scores/stats` | สถิติคะแนน |
| POST | `/api/courses/:courseId/exam-scores` | บันทึกคะแนน |
| POST | `/api/courses/:courseId/exam-scores/bulk` | บันทึก bulk |
| DELETE | `/api/courses/:courseId/exam-scores/:scoreId` | ลบ |

---

## 13. ระบบเช็คชื่อ

### คำอธิบาย
ระบบเช็คชื่อนักศึกษาแบบ real-time รองรับการยืนยันตัวตนผ่าน Google, PIN code และตำแหน่ง GPS

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| สร้างรอบเช็คชื่อ | ตั้งชื่อ, กำหนดเวลา, เลือก Section, ตั้ง PIN |
| ประเภทการเรียน | lecture, lab, online |
| เปิด/ปิดรอบ | Activate เพื่อรับเช็คชื่อ, Close เมื่อเสร็จ |
| นักศึกษาเช็คชื่อ | เช็คชื่อผ่านหน้าเว็บ ยืนยันด้วย Google account |
| ตรวจสอบตำแหน่ง | ตรวจสอบ GPS ว่าอยู่ในรัศมีที่กำหนด |
| เกณฑ์เวลาสาย | กำหนดนาทีที่ถือว่า "สาย" |
| แก้ไขสถานะ | อาจารย์/TA แก้ไขสถานะ (มา/สาย/ลา/ขาด) พร้อม track ผู้แก้ไข |
| Preview การเปลี่ยน Section | แสดงผลกระทบก่อนลบ Section ออกจากรอบเช็คชื่อ |
| Preview การเปลี่ยนเวลา | แสดงผลกระทบก่อนเปลี่ยนเกณฑ์เวลา |
| Live Monitoring | แสดงผลแบบ real-time ขณะเปิดรอบเช็คชื่อ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/attendance` | ดูรอบเช็คชื่อทั้งหมด |
| POST | `/api/attendance` | สร้างรอบใหม่ |
| GET | `/api/attendance/:id` | ดูรายละเอียดรอบ |
| PUT | `/api/attendance/:id` | แก้ไข |
| DELETE | `/api/attendance/:id` | ลบ |
| POST | `/api/attendance/:id/activate` | เปิดรอบ |
| POST | `/api/attendance/:id/close` | ปิดรอบ |
| GET | `/api/attendance/:id/records` | ดูผลเช็คชื่อ |
| PUT | `/api/attendance/:id/records/:recordId` | แก้ไขสถานะเช็คชื่อ |
| POST | `/api/attendance/:id/preview-section-change` | ดูผลกระทบก่อนลบ Section |
| POST | `/api/attendance/:id/preview-time-change` | ดูผลกระทบก่อนเปลี่ยนเวลา |
| POST | `/api/attendance/:id/apply-time-change` | บังคับใช้การเปลี่ยนเวลา |
| GET | `/api/attendance/check-in/:sessionId/info` | ข้อมูลรอบสำหรับเช็คชื่อ (public) |
| POST | `/api/attendance/check-in/:sessionId` | เช็คชื่อ (public) |
| POST | `/api/attendance/verify-student` | ตรวจสอบนักศึกษาจาก Google email |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/check-in/[sessionId]` | หน้าเช็คชื่อสำหรับนักศึกษา |
| `/attendance/[id]/session/[sessionId]/live` | มอนิเตอร์เช็คชื่อแบบ live |

---

## 14. ระบบจองคิว/ตรวจงาน

### คำอธิบาย
ระบบจองคิวเพื่อให้นักศึกษาจองโต๊ะสำหรับตรวจงาน/ขอความช่วยเหลือ พร้อมหน้า Projector แสดงสถานะโต๊ะแบบ real-time

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| สร้าง Queue Session | สร้างรอบจองคิว เลือกห้อง, ตั้ง PIN, เชื่อม Assignment |
| เปิด/หยุด/ปิด Session | จัดการสถานะ (draft → active → paused → closed) |
| นักศึกษาจอง | จองคิวด้วย PIN เลือกโต๊ะและประเภท (ตรวจงาน/ขอช่วยเหลือ) |
| TA เข้าทำงาน | TA join เป็น Worker เพื่อรับงาน |
| มอบหมายอัตโนมัติ | ระบบจัดสรรคิวให้ Worker อัตโนมัติ (background worker) |
| ตรวจเสร็จ/ข้าม | Worker กด complete หรือ skip คิว พร้อมให้คะแนน |
| ยกเลิกคิว | นักศึกษาหรืออาจารย์ยกเลิกคิว |
| Projector View | หน้าแสดงผลบนจอ projector สถานะโต๊ะแบบ real-time |
| Push Notification | แจ้งเตือนนักศึกษาเมื่อถึงคิว |
| Real-time Sync | สถานะอัปเดตแบบ real-time ผ่าน Socket.IO |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `.../queue/sessions` | ดู session ทั้งหมด |
| POST | `.../queue/sessions` | สร้าง session |
| GET | `.../queue/sessions/:sessionId` | ดูรายละเอียด |
| PUT | `.../queue/sessions/:sessionId` | แก้ไข |
| POST | `.../queue/sessions/:sessionId/status` | เปลี่ยนสถานะ |
| DELETE | `.../queue/sessions/:sessionId` | ลบ |
| POST | `.../queue/sessions/:sessionId/regenerate-pin` | สร้าง PIN ใหม่ |
| POST | `.../queue/sessions/:sessionId/workers/join` | TA join เป็น Worker |
| POST | `.../queue/sessions/:sessionId/workers/leave` | TA ออก |
| GET | `.../queue/sessions/:sessionId/workers` | ดู Worker ทั้งหมด |
| GET | `.../queue/sessions/:sessionId/workers/current-booking` | ดูงานปัจจุบันของ Worker |
| GET | `.../queue/sessions/:sessionId/bookings` | ดูคิวทั้งหมด |
| POST | `.../queue/sessions/:sessionId/bookings/:bookingId/complete` | ตรวจเสร็จ |
| POST | `.../queue/sessions/:sessionId/bookings/:bookingId/skip` | ข้ามคิว |
| POST | `/api/queue/verify-pin` | ตรวจสอบ PIN (public) |
| POST | `/api/queue/validate` | ตรวจสอบข้อมูลก่อนจอง |
| POST | `/api/queue/check-existing` | เช็คคิวที่มีอยู่ |
| POST | `/api/queue/bookings` | จองคิว (public) |
| GET | `/api/queue/bookings/:bookingId/status` | ดูสถานะคิว |
| POST | `/api/queue/bookings/:bookingId/cancel` | ยกเลิกคิว |
| GET | `/api/queue/sessions/:sessionId/desk-statuses` | สถานะโต๊ะ (projector) |
| POST | `/api/queue/sessions/:sessionId/status` | เปลี่ยนสถานะ (projector) |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/queue/book` | หน้าจองคิวสำหรับนักศึกษา |
| `/queue/projector/[sessionId]` | หน้า Projector แสดงสถานะโต๊ะ |

---

## 15. การจัดการทีม/กลุ่ม

### คำอธิบาย
สร้างและจัดการกลุ่มนักศึกษา รองรับทั้งกลุ่มถาวรและกลุ่มรายสัปดาห์ สามารถสุ่มจัดกลุ่มได้

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| สร้างกลุ่ม | สร้างกลุ่มใหม่ (ถาวร/รายสัปดาห์) |
| สร้างกลุ่ม bulk | สุ่มจัดกลุ่มอัตโนมัติ |
| เพิ่ม/ลบสมาชิก | เพิ่มหรือนำนักศึกษาออกจากกลุ่ม |
| แก้ไข/ลบกลุ่ม | แก้ไขชื่อหรือลบกลุ่ม |
| ลบกลุ่ม bulk | ลบหลายกลุ่มพร้อมกัน |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/courses/:id/teams` | ดูกลุ่มทั้งหมด |
| POST | `/api/courses/:id/teams` | สร้างกลุ่ม |
| POST | `/api/courses/:id/teams/bulk` | สุ่มสร้างกลุ่ม |
| POST | `/api/courses/:id/teams/bulk-delete` | ลบกลุ่ม bulk |
| PUT | `/api/courses/:id/teams/:teamId` | แก้ไขกลุ่ม |
| DELETE | `/api/courses/:id/teams/:teamId` | ลบกลุ่ม |
| POST | `/api/courses/:id/teams/:teamId/members` | เพิ่มสมาชิก |
| DELETE | `/api/courses/:id/teams/:teamId/members/:studentId` | ลบสมาชิก |

---

## 16. ระบบ Feedback

### คำอธิบาย
ผู้ใช้สามารถส่ง feedback (แจ้งบัก, เสนอฟีเจอร์, ปรับปรุง) ให้ admin ตรวจสอบและตอบกลับ

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ส่ง Feedback | ส่งพร้อมประเภท, หัวข้อ, รายละเอียด, ไฟล์แนบ |
| ดู Feedback ตนเอง | ดูรายการที่ส่งไปพร้อมสถานะ |
| จัดการ Feedback (admin) | ดูทั้งหมด, filter, ตอบกลับ, เปลี่ยนสถานะ |
| สถิติ | จำนวน feedback แต่ละประเภท/สถานะ |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| POST | `/api/feedback` | ส่ง feedback |
| GET | `/api/feedback/my` | ดูของตนเอง |
| GET | `/api/feedback/stats` | สถิติ (admin) |
| GET | `/api/feedback` | ดูทั้งหมด (admin) |
| GET | `/api/feedback/:id` | ดูรายตัว |
| PUT | `/api/feedback/:id` | ตอบกลับ/อัปเดต |
| DELETE | `/api/feedback/:id` | ลบ |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/admin/feedback` | หน้าจัดการ Feedback |

---

## 17. การแจ้งเตือน Push Notification

### คำอธิบาย
ระบบแจ้งเตือนผ่าน Firebase Cloud Messaging (FCM) สำหรับแจ้งสถานะคิวให้นักศึกษาและ Worker

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| ลงทะเบียน Token | ลงทะเบียน FCM token ของอุปกรณ์ |
| ยกเลิกการลงทะเบียน | ยกเลิก FCM token |
| แจ้งเตือนคิว | แจ้งเตือนเมื่อถึงคิว, ตรวจเสร็จ, session ปิด |
| บันทึกการส่ง | เก็บ log การส่ง notification ทุกครั้ง |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| POST | `/api/notifications/register` | ลงทะเบียน token |
| POST | `/api/notifications/unregister` | ยกเลิก token |
| POST | `/api/notifications/update-booking` | อัปเดต booking ref |
| GET | `/api/notifications/tokens` | ดู token ที่ลงทะเบียน |
| GET | `/api/notifications/logs` | ดู log การส่ง |

---

## 18. บันทึกกิจกรรมรายวิชา

### คำอธิบาย
บันทึกทุกกิจกรรมที่เกิดขึ้นภายในรายวิชา เป็น audit trail สำหรับอาจารย์ตรวจสอบ

### ฟังก์ชันหลัก

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| Activity Log | บันทึกทุก action (สร้าง, แก้ไข, ลบ, ให้คะแนน ฯลฯ) |
| สถิติ | สรุปจำนวนกิจกรรมแต่ละหมวด |
| สถิติ TA | ดูผลงาน TA (จำนวนครั้งที่ให้คะแนน, เช็คชื่อ ฯลฯ) |
| Filter | กรองตามหมวดหมู่, ผู้กระทำ, ช่วงเวลา |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `.../activity-logs` | ดู log กิจกรรม |
| GET | `.../activity-logs/stats` | สถิติ |
| GET | `.../activity-logs/filters` | ตัวเลือกสำหรับ filter |
| GET | `.../activity-logs/ta-stats` | สถิติ TA ทั้งหมด |
| GET | `.../activity-logs/ta-stats/:userId` | สถิติ TA รายบุคคล |

---

## 19. การบริหารระบบ

### คำอธิบาย
เครื่องมือสำหรับ Admin ในการตรวจสอบสุขภาพของระบบ, ดู log, และ monitoring แบบ real-time

### 19.1 System Metrics

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| CPU Usage | ดูการใช้งาน CPU |
| Memory Usage | ดูการใช้งาน RAM |
| Disk Usage | ดูพื้นที่ disk |
| Server Info | ข้อมูลเซิร์ฟเวอร์ |

### 19.2 System Logs (พ.ร.บ. คอมพิวเตอร์ Compliance)

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| บันทึก log ครบถ้วน | บันทึก access, error, auth, security log ตาม พ.ร.บ. |
| Filter/Search | ค้นหาและกรอง log ตามเงื่อนไข |
| Timeline | แสดง log เป็นกราฟตามเวลา |
| Export CSV | ส่งออก log เป็นไฟล์ CSV |
| Error/Security | ดู error ล่าสุดและเหตุการณ์ security |
| Log Cleanup | ล้าง log เก่า |

### 19.3 Monitoring (Prometheus Integration)

| ฟังก์ชัน | คำอธิบาย |
|---|---|
| Prometheus Metrics | Expose metrics สำหรับ Prometheus scrape |
| System Monitoring | CPU, RAM, disk, network, load average |
| Container Monitoring | สถานะและ resource ของ Docker containers |
| Website Monitoring | Uptime, response time, error rate, status codes |
| Alertmanager Webhook | รับ alert จาก Alertmanager |

### API Endpoints

| Method | Endpoint | คำอธิบาย |
|---|---|---|
| GET | `/api/system/metrics` | System metrics ทั้งหมด |
| GET | `/api/system/cpu` | CPU usage |
| GET | `/api/system/memory` | Memory usage |
| GET | `/api/system/info` | Server info |
| GET | `/api/logs` | ดู log ทั้งหมด |
| GET | `/api/logs/filters` | ตัวเลือก filter |
| GET | `/api/logs/stats` | สถิติ log |
| GET | `/api/logs/timeline` | Timeline |
| GET | `/api/logs/export` | Export CSV |
| GET | `/api/logs/errors/recent` | Error ล่าสุด |
| GET | `/api/logs/security/recent` | Security events ล่าสุด |
| GET | `/api/logs/:id` | ดู log รายตัว |
| POST | `/api/logs/cleanup` | Cleanup log เก่า |
| GET | `/api/metrics/prometheus` | Prometheus scrape endpoint |

### หน้าจอ (Frontend)

| เส้นทาง | คำอธิบาย |
|---|---|
| `/admin/dashboard` | หน้า dashboard admin |
| `/admin/logs` | หน้าดู system logs |

---

## 20. การสื่อสาร Real-time

### คำอธิบาย
ใช้ Socket.IO สำหรับสื่อสารแบบ real-time ใน feature ที่ต้องการอัปเดตทันที

### Events หลัก

| Event | คำอธิบาย |
|---|---|
| Queue Status Update | อัปเดตสถานะคิวแบบ real-time (สถานะโต๊ะ, คิวใหม่, คิวเสร็จ) |
| Session Status Changed | แจ้งเมื่อสถานะ session (active/paused/closed) เปลี่ยน |
| Attendance Live | อัปเดตผลเช็คชื่อแบบ real-time |
| Desk Status Broadcasting | broadcast สถานะโต๊ะไปยังหน้า Projector |

### Context & Infrastructure

| Component | คำอธิบาย |
|---|---|
| SocketContext | React Context จัดการ Socket.IO connection ผ่าน `useSocket()` hook |
| Standalone Sockets | Socket connections แยกสำหรับ Projector และ Worker pages |
| Room-based | ใช้ Socket.IO rooms (เช่น `queue-{sessionId}`) สำหรับ targeted events |

---

## 21. โครงสร้างพื้นฐาน DevOps

### คำอธิบาย
ระบบ deployment และ monitoring infrastructure

### เครื่องมือ

| เครื่องมือ | คำอธิบาย |
|---|---|
| Docker Compose | จัดการ containers (dev/prod/db) |
| Jenkins | CI/CD pipeline อัตโนมัติ (dev + prod) |
| Prometheus | เก็บ metrics ของระบบ |
| Grafana | แสดงผล dashboard สำหรับ monitoring |
| Loki | เก็บ log จาก containers |
| Promtail | ส่ง log ไปยัง Loki |
| Alertmanager | จัดการ alerts เมื่อเกิดปัญหา |

---

## สรุปภาพรวม

| # | Feature Area | ความสามารถหลัก |
|:---:|---|---|
| 1 | **Authentication** | Login, JWT, OAuth (Google/GitHub/Apple), Password Reset, Avatar |
| 2 | **Two-Factor Auth** | TOTP, Email 2FA, Backup Codes |
| 3 | **User Management** | Admin CRUD, Role Management, Status Toggle |
| 4 | **Student Management** | CRUD, CSV/Excel Import, Bulk Search, Score Lookup |
| 5 | **Course Management** | CRUD, Sections, TA/Instructor Assignment, Student Enrollment |
| 6 | **Classroom Management** | CRUD, Canvas Editor, Desks, Zones |
| 7 | **Assignment Management** | CRUD, Sub-items, Categories, Drag-and-drop Reorder |
| 8 | **Scoring System** | Individual/Bulk/Group Scoring, Score Matrix |
| 9 | **Score Edit Requests** | TA → Instructor Approval, Image Attachments, Batch |
| 10 | **Bonus Scores** | In-class Bonus Points, History, Summary |
| 11 | **Exam Scores** | Midterm/Final, Configurable Weights, Bulk Import, Statistics |
| 12 | **Attendance System** | Self Check-in (Google), PIN/GPS Verification, Live Monitor |
| 13 | **Queue System** | PIN Booking, Auto Assignment, Projector Display, FCM Push |
| 14 | **Team/Group Management** | Manual/Random Formation, Bulk Operations |
| 15 | **Feedback System** | Submission, Admin Review, Statistics |
| 16 | **Push Notifications** | FCM Registration, Booking Notifications, Delivery Logs |
| 17 | **Course Activity Logs** | Audit Trail, TA Stats, Filters |
| 18 | **System Logs** | พ.ร.บ. Compliance, Export, Timeline, Cleanup |
| 19 | **System Monitoring** | Prometheus, CPU/RAM/Disk, Containers, Website Uptime |
| 20 | **Real-time Features** | Socket.IO, Queue Updates, Attendance Live, Desk Broadcasting |
| 21 | **DevOps** | Docker Compose, Jenkins CI/CD, Prometheus/Grafana/Loki Stack |

---

## เทคโนโลยีที่ใช้

| ส่วน | เทคโนโลยี |
|---|---|
| **Frontend** | Next.js 16, TypeScript, HeroUI 3 (NextUI), Tailwind CSS 4 |
| **Backend** | Node.js, Express.js, Sequelize ORM |
| **Database** | PostgreSQL |
| **Real-time** | Socket.IO |
| **Authentication** | JWT, Passport.js (Google/GitHub/Apple OAuth), TOTP (speakeasy) |
| **Push Notifications** | Firebase Cloud Messaging (FCM) |
| **File Upload** | Multer |
| **Monitoring** | Prometheus, Grafana, Loki, Alertmanager |
| **DevOps** | Docker, Docker Compose, Jenkins |
| **Background Worker** | Custom Queue Assignment Worker (Node.js) |

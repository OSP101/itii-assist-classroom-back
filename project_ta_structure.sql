-- phpMyAdmin SQL Dump
-- version 5.2.3
-- https://www.phpmyadmin.net/
--
-- Host: db:3306
-- Generation Time: Apr 12, 2026 at 05:32 PM
-- Server version: 8.0.45
-- PHP Version: 8.3.30

SET SQL_MODE = "NO_AUTO_VALUE_ON_ZERO";
START TRANSACTION;
SET time_zone = "+00:00";


/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!40101 SET NAMES utf8mb4 */;

--
-- Database: `project_ta_prod`
--

-- --------------------------------------------------------

--
-- Table structure for table `assignments`
--

CREATE TABLE `assignments` (
  `id` int NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `assignment_type` enum('individual','permanent_group','weekly_group','assignment') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'individual' COMMENT 'ประเภท: individual=ปฏิบัติการเดี่ยว(Lab), permanent_group=กลุ่มถาวร, weekly_group=กลุ่มรายสัปดาห์, assignment=การบ้าน',
  `week_number` int DEFAULT NULL COMMENT 'สำหรับงานกลุ่มรายสัปดาห์',
  `linked_attendance_session_id` bigint DEFAULT NULL,
  `attendance_condition` enum('and','or') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'or',
  `max_score` decimal(5,2) NOT NULL DEFAULT '10.00',
  `due_date` datetime DEFAULT NULL,
  `is_active` tinyint(1) DEFAULT '1',
  `created_by` bigint NOT NULL,
  `order_index` int DEFAULT '0',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `is_score_visible` tinyint(1) DEFAULT '1'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `assignment_attendance_links`
--

CREATE TABLE `assignment_attendance_links` (
  `id` int NOT NULL,
  `assignment_id` int NOT NULL,
  `attendance_session_id` bigint NOT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `assignment_sub_items`
--

CREATE TABLE `assignment_sub_items` (
  `id` int NOT NULL,
  `assignment_id` int NOT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `max_score` decimal(10,2) DEFAULT '10.00',
  `order_index` int DEFAULT '0',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `attendance_records`
--

CREATE TABLE `attendance_records` (
  `id` bigint NOT NULL,
  `attendance_session_id` bigint NOT NULL,
  `student_id` bigint NOT NULL,
  `check_in_time` datetime DEFAULT NULL,
  `status` enum('present','late','leave','absent') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'absent',
  `google_email` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `google_id` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `pin_verified` tinyint(1) DEFAULT '0',
  `location_verified` tinyint(1) DEFAULT '0',
  `note` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `location_lat` decimal(10,7) DEFAULT NULL,
  `location_lng` decimal(10,7) DEFAULT NULL,
  `distance_meters` int DEFAULT NULL,
  `updated_by` bigint DEFAULT NULL,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `attendance_sessions`
--

CREATE TABLE `attendance_sessions` (
  `id` bigint NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `course_section_id` bigint DEFAULT NULL,
  `title` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'Attendance',
  `pin_code` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `session_type` enum('lecture','lab','online') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'lecture',
  `check_location` tinyint(1) DEFAULT '0',
  `location_lat` decimal(10,7) DEFAULT NULL,
  `location_lng` decimal(10,7) DEFAULT NULL,
  `radius_meters` int DEFAULT '50',
  `start_time` datetime NOT NULL,
  `end_time` datetime NOT NULL,
  `late_threshold_minutes` int DEFAULT '15',
  `late_threshold_time` time DEFAULT NULL COMMENT 'เวลาที่ถือว่าสาย (เช่น 08:15:00)',
  `status` enum('draft','active','closed') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'draft',
  `created_by` bigint DEFAULT NULL,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `attendance_session_sections`
--

CREATE TABLE `attendance_session_sections` (
  `id` bigint NOT NULL,
  `attendance_session_id` bigint NOT NULL,
  `course_section_id` bigint NOT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `bonus_scores`
--

CREATE TABLE `bonus_scores` (
  `id` bigint NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `student_id` bigint NOT NULL,
  `score` decimal(5,2) NOT NULL DEFAULT '1.00',
  `reason` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'เหตุผลการให้คะแนน เช่น ตอบคำถามในห้องเรียน',
  `given_by` bigint NOT NULL COMMENT 'ผู้ให้คะแนน (instructor/ta)',
  `given_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='ตารางเก็บคะแนนพิเศษจากการถามตอบในห้องเรียน';

-- --------------------------------------------------------

--
-- Table structure for table `classrooms`
--

CREATE TABLE `classrooms` (
  `id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'à¸Šà¸·à¹ˆà¸­à¸«à¹‰à¸­à¸‡ à¹€à¸Šà¹ˆà¸™ à¸«à¹‰à¸­à¸‡ 306',
  `building` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'à¸­à¸²à¸„à¸²à¸£ à¹€à¸Šà¹ˆà¸™ à¸­à¸²à¸„à¸²à¸£ IT',
  `floor` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'à¸Šà¸±à¹‰à¸™',
  `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT 'à¸£à¸²à¸¢à¸¥à¸°à¹€à¸­à¸µà¸¢à¸”à¹€à¸žà¸´à¹ˆà¸¡à¹€à¸•à¸´à¸¡',
  `is_deleted` tinyint(1) NOT NULL DEFAULT '0' COMMENT 'Soft delete flag',
  `is_active` tinyint(1) NOT NULL DEFAULT '1' COMMENT 'สถานะเปิด/ปิดใช้งาน',
  `created_by` bigint DEFAULT NULL COMMENT 'à¸œà¸¹à¹‰à¸ªà¸£à¹‰à¸²à¸‡',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `courses`
--

CREATE TABLE `courses` (
  `id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `code` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `year` smallint NOT NULL,
  `semester` tinyint NOT NULL,
  `instructor_id` bigint DEFAULT NULL,
  `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `image` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `is_active` tinyint(1) DEFAULT '1',
  `attention_threshold` tinyint UNSIGNED NOT NULL DEFAULT '60' COMMENT 'Percentage threshold for low performer alert (default 60%)'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `course_activity_logs`
--

CREATE TABLE `course_activity_logs` (
  `id` bigint NOT NULL,
  `course_id` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'รหัสวิชาที่เกิดกิจกรรม',
  `actor_user_id` bigint NOT NULL COMMENT 'ผู้กระทำ (user id)',
  `action` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'ประเภทการกระทำ',
  `category` varchar(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'general' COMMENT 'หมวดหมู่',
  `target_type` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'ประเภทเป้าหมาย เช่น student, assignment, score',
  `target_id` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'ID ของเป้าหมาย',
  `target_name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'ชื่อเป้าหมาย เช่น ชื่องาน, ชื่อนักศึกษา',
  `detail` json DEFAULT NULL COMMENT 'รายละเอียดเพิ่มเติม',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='บันทึกกิจกรรมภายในรายวิชา';

-- --------------------------------------------------------

--
-- Table structure for table `course_instructors`
--

CREATE TABLE `course_instructors` (
  `id` int NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` bigint NOT NULL,
  `is_primary` tinyint(1) NOT NULL DEFAULT '0',
  `assigned_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `course_sections`
--

CREATE TABLE `course_sections` (
  `id` bigint NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `section_no` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `note` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `course_section_students`
--

CREATE TABLE `course_section_students` (
  `id` bigint NOT NULL,
  `course_section_id` bigint NOT NULL,
  `student_id` bigint NOT NULL,
  `enrolled_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `course_tas`
--

CREATE TABLE `course_tas` (
  `id` bigint NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` bigint NOT NULL,
  `assigned_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `desks`
--

CREATE TABLE `desks` (
  `id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `classroom_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `number` int NOT NULL COMMENT 'à¸«à¸¡à¸²à¸¢à¹€à¸¥à¸‚à¹‚à¸•à¹Šà¸°',
  `x` int NOT NULL DEFAULT '0' COMMENT 'à¸•à¸³à¹à¸«à¸™à¹ˆà¸‡ X à¸šà¸™ canvas',
  `y` int NOT NULL DEFAULT '0' COMMENT 'à¸•à¸³à¹à¸«à¸™à¹ˆà¸‡ Y à¸šà¸™ canvas',
  `type` enum('computer','normal','teacher') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'normal' COMMENT 'ประเภทโต๊ะ',
  `is_enabled` tinyint(1) NOT NULL DEFAULT '1' COMMENT 'à¹€à¸›à¸´à¸”/à¸›à¸´à¸”à¹ƒà¸Šà¹‰à¸‡à¸²à¸™',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `exam_scores`
--

CREATE TABLE `exam_scores` (
  `id` int NOT NULL COMMENT 'Primary Key',
  `exam_setting_id` int NOT NULL COMMENT 'FK->exam_settings: การตั้งค่าการสอบ',
  `student_id` bigint NOT NULL COMMENT 'FK->students: นักศึกษา',
  `score` decimal(5,2) DEFAULT NULL COMMENT 'คะแนนที่ได้',
  `comment` text COLLATE utf8mb4_unicode_ci COMMENT 'หมายเหตุ',
  `graded_by` bigint DEFAULT NULL COMMENT 'FK->users: ผู้ให้คะแนน',
  `graded_at` datetime DEFAULT NULL COMMENT 'วันเวลาที่ให้คะแนน',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP COMMENT 'วันที่สร้าง',
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'วันที่แก้ไขล่าสุด'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `exam_settings`
--

CREATE TABLE `exam_settings` (
  `id` int NOT NULL COMMENT 'Primary Key',
  `course_id` varchar(21) COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'FK->courses: รายวิชา',
  `exam_type` enum('midterm','final') COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'ประเภทการสอบ: midterm=กลางภาค, final=ปลายภาค',
  `component` enum('lab','lecture') COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'องค์ประกอบ: lab=ปฏิบัติการ, lecture=บรรยาย',
  `max_score` decimal(5,2) NOT NULL DEFAULT '0.00' COMMENT 'คะแนนเต็ม',
  `is_visible` tinyint(1) NOT NULL DEFAULT '0' COMMENT 'แสดงผลให้นักศึกษาเห็นหรือไม่: 0=ซ่อน, 1=แสดง',
  `is_active` tinyint(1) NOT NULL DEFAULT '1' COMMENT 'เปิดใช้งานหรือไม่: 0=ปิด, 1=เปิด',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP COMMENT 'วันที่สร้าง',
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'วันที่แก้ไขล่าสุด'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `fcm_tokens`
--

CREATE TABLE `fcm_tokens` (
  `id` bigint NOT NULL,
  `fcm_token` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_type` enum('worker','student') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` bigint DEFAULT NULL COMMENT 'For authenticated users (workers)',
  `student_id` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'For students (รหัสนักศึกษา)',
  `booking_id` bigint DEFAULT NULL COMMENT 'For students - linked to their booking',
  `session_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'For workers - linked to queue session',
  `device_info` json DEFAULT NULL COMMENT 'Browser/device information',
  `is_active` tinyint(1) DEFAULT '1',
  `last_used_at` timestamp NULL DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `feedbacks`
--

CREATE TABLE `feedbacks` (
  `id` bigint NOT NULL,
  `user_id` bigint DEFAULT NULL,
  `type` enum('bug','feature','improvement','other') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'other' COMMENT 'bug=รายงานข้อผิดพลาด, feature=ขอฟีเจอร์ใหม่, improvement=ข้อเสนอแนะ, other=อื่นๆ',
  `title` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `attachments` json DEFAULT NULL COMMENT 'Array of file URLs (images/videos)',
  `status` enum('pending','reviewing','resolved','rejected') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'pending',
  `priority` enum('low','medium','high','critical') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'medium',
  `admin_notes` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT 'Notes from admin/developer',
  `resolved_at` datetime DEFAULT NULL,
  `resolved_by` bigint DEFAULT NULL,
  `contact_email` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL COMMENT 'Email for anonymous feedback',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `notification_logs`
--

CREATE TABLE `notification_logs` (
  `id` bigint NOT NULL,
  `fcm_token_id` bigint DEFAULT NULL,
  `notification_type` enum('new-task','queue-ready','booking-completed','session-closed','other') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `title` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `body` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `data` json DEFAULT NULL,
  `status` enum('pending','sent','failed','delivered') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'pending',
  `error_message` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `sent_at` timestamp NULL DEFAULT NULL,
  `delivered_at` timestamp NULL DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `password_reset_tokens`
--

CREATE TABLE `password_reset_tokens` (
  `id` bigint NOT NULL,
  `user_id` bigint NOT NULL,
  `token` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `expires_at` datetime NOT NULL,
  `used_at` datetime DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `queue_bookings`
--

CREATE TABLE `queue_bookings` (
  `id` bigint NOT NULL,
  `queue_session_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `student_id` bigint NOT NULL COMMENT 'FK ไปยัง students table',
  `desk_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `desk_number` int NOT NULL COMMENT 'เลขโต๊ะ (denormalized)',
  `booking_type` enum('grading','help') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'grading=ตรวจงาน, help=ขอความช่วยเหลือ',
  `queue_number` int NOT NULL COMMENT 'หมายเลขคิวในรอบนี้',
  `note` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT 'หมายเหตุเพิ่มเติม เช่น ปัญหาที่พบ',
  `status` enum('waiting','in_progress','completed','cancelled','no_show') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'waiting' COMMENT 'waiting=รอคิว, in_progress=กำลังตรวจ, completed=เสร็จ, cancelled=ยกเลิก, no_show=ไม่มา',
  `assigned_worker_id` bigint DEFAULT NULL COMMENT 'ผู้ที่ได้รับมอบหมายตรวจ',
  `assigned_at` timestamp NULL DEFAULT NULL COMMENT 'เวลาที่ได้รับมอบหมาย',
  `started_at` timestamp NULL DEFAULT NULL COMMENT 'เวลาเริ่มตรวจ',
  `completed_at` timestamp NULL DEFAULT NULL COMMENT 'เวลาตรวจเสร็จ',
  `score` decimal(5,2) DEFAULT NULL COMMENT 'คะแนนที่ได้',
  `score_comment` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT 'ความเห็นเรื่องคะแนน',
  `worker_note` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT 'บันทึกจากผู้ตรวจ',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `queue_desk_status`
--

CREATE TABLE `queue_desk_status` (
  `id` bigint NOT NULL,
  `queue_session_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `desk_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `grading_status` enum('not_started','waiting','in_progress','completed') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'not_started' COMMENT 'not_started=ยังไม่จอง, waiting=รอตรวจ, in_progress=กำลังตรวจ, completed=ตรวจแล้ว',
  `grading_booking_id` bigint DEFAULT NULL COMMENT 'Booking ID ปัจจุบันของ grading',
  `help_status` enum('none','waiting','in_progress') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'none' COMMENT 'none=ไม่มี, waiting=รอช่วยเหลือ, in_progress=กำลังช่วย',
  `help_booking_id` bigint DEFAULT NULL COMMENT 'Booking ID ปัจจุบันของ help',
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `queue_sessions`
--

CREATE TABLE `queue_sessions` (
  `id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `classroom_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `title` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'ชื่อการจองคิว เช่น Lab01 - ตรวจงาน',
  `description` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT 'รายละเอียดเพิ่มเติม',
  `pin_code` varchar(10) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'รหัส PIN 6 หลัก',
  `linked_assignment_id` int DEFAULT NULL COMMENT 'Assignment ที่ลิงก์สำหรับลงคะแนน',
  `require_attendance` tinyint(1) DEFAULT '0' COMMENT 'ต้องเช็คชื่อก่อนจึงจะจองได้',
  `linked_attendance_session_id` bigint DEFAULT NULL COMMENT 'Session เช็คชื่อที่ลิงก์',
  `status` enum('draft','active','paused','closed') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'draft' COMMENT 'draft=ยังไม่เปิด, active=กำลังรับจอง, paused=หยุดชั่วคราว, closed=ปิดแล้ว',
  `start_time` datetime DEFAULT NULL COMMENT 'เวลาเริ่มรับจอง',
  `end_time` datetime DEFAULT NULL COMMENT 'เวลาสิ้นสุดรับจอง',
  `created_by` bigint DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `queue_workers`
--

CREATE TABLE `queue_workers` (
  `id` bigint NOT NULL,
  `queue_session_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` bigint NOT NULL COMMENT 'อาจารย์หรือ TA',
  `accept_grading` tinyint(1) DEFAULT '1' COMMENT 'รับตรวจงาน',
  `accept_help` tinyint(1) DEFAULT '1' COMMENT 'รับแก้ไขปัญหา',
  `push_notifications_enabled` tinyint(1) DEFAULT '1',
  `status` enum('online','busy','offline') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'offline' COMMENT 'online=พร้อมรับงาน, busy=กำลังทำงาน, offline=ออฟไลน์',
  `current_booking_id` bigint DEFAULT NULL COMMENT 'งานที่กำลังทำอยู่',
  `total_grading_completed` int DEFAULT '0' COMMENT 'จำนวนตรวจงานเสร็จ',
  `total_help_completed` int DEFAULT '0' COMMENT 'จำนวนช่วยเหลือเสร็จ',
  `last_active_at` timestamp NULL DEFAULT NULL COMMENT 'เวลาที่ active ล่าสุด',
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `refresh_tokens`
--

CREATE TABLE `refresh_tokens` (
  `id` bigint NOT NULL,
  `jti` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `user_id` bigint NOT NULL,
  `revoked` tinyint(1) DEFAULT '0',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `expires_at` datetime NOT NULL,
  `meta` json DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `scores`
--

CREATE TABLE `scores` (
  `id` int NOT NULL,
  `assignment_id` int NOT NULL,
  `student_id` bigint DEFAULT NULL COMMENT 'สำหรับงานเดี่ยว',
  `group_id` bigint DEFAULT NULL COMMENT 'สำหรับงานกลุ่ม',
  `sub_item_id` int DEFAULT NULL,
  `score` decimal(5,2) DEFAULT NULL,
  `comment` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `graded_by` bigint DEFAULT NULL,
  `graded_at` datetime DEFAULT NULL,
  `status` enum('pending','graded') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'pending',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `score_edit_requests`
--

CREATE TABLE `score_edit_requests` (
  `id` int NOT NULL,
  `score_id` int NOT NULL,
  `old_score` decimal(5,2) DEFAULT NULL,
  `new_score` decimal(5,2) NOT NULL,
  `reason` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `requested_by` bigint NOT NULL,
  `status` enum('pending','approved','rejected') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'pending',
  `reviewed_by` bigint DEFAULT NULL,
  `reviewed_at` datetime DEFAULT NULL,
  `review_comment` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `images` json DEFAULT NULL COMMENT 'JSON array of image file paths'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `students`
--

CREATE TABLE `students` (
  `id` bigint NOT NULL,
  `student_id` varchar(11) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `full_name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `email` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `extra` json DEFAULT NULL,
  `is_active` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `student_groups`
--

CREATE TABLE `student_groups` (
  `id` bigint NOT NULL,
  `course_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `group_type` enum('permanent','temporary') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT 'permanent',
  `week_number` int DEFAULT NULL,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `student_group_members`
--

CREATE TABLE `student_group_members` (
  `id` bigint NOT NULL,
  `group_id` bigint NOT NULL,
  `student_id` bigint NOT NULL,
  `joined_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `system_logs`
--

CREATE TABLE `system_logs` (
  `id` bigint NOT NULL,
  `log_type` enum('access','error','auth','security') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'access',
  `severity` enum('debug','info','warn','error','critical') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'info',
  `actor_user_id` bigint DEFAULT NULL,
  `session_id` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `auth_method` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `action` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `http_method` varchar(10) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `url` varchar(2048) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `query_params` json DEFAULT NULL,
  `status_code` int DEFAULT NULL,
  `response_time_ms` int DEFAULT NULL,
  `detail` json DEFAULT NULL,
  `error_message` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `error_stack` text CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `error_code` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `resource_type` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `resource_id` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `request_body` json DEFAULT NULL,
  `request_size` int DEFAULT NULL,
  `response_size` int DEFAULT NULL,
  `ip_address` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `user_agent` varchar(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `referer` varchar(2048) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `device_type` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `browser` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `os` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `two_factor_pending`
--

CREATE TABLE `two_factor_pending` (
  `id` bigint NOT NULL,
  `user_id` bigint NOT NULL,
  `method` varchar(20) COLLATE utf8mb4_unicode_ci NOT NULL,
  `secret` text COLLATE utf8mb4_unicode_ci NOT NULL,
  `email_code` varchar(6) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `email_code_expires_at` timestamp NULL DEFAULT NULL,
  `created_at` timestamp NULL DEFAULT CURRENT_TIMESTAMP,
  `expires_at` timestamp NULL DEFAULT ((now() + interval 15 minute))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `users`
--

CREATE TABLE `users` (
  `id` bigint NOT NULL,
  `username` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `password_hash` char(60) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `role` enum('admin','instructor','ta') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `full_name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `email` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `google_id` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `provider` enum('local','google') CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'local',
  `is_active` tinyint(1) NOT NULL DEFAULT '1',
  `must_change_password` tinyint(1) NOT NULL DEFAULT '0',
  `avatar` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
  `two_factor_enabled` tinyint(1) DEFAULT '0',
  `two_factor_method` varchar(20) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `two_factor_secret` text COLLATE utf8mb4_unicode_ci,
  `two_factor_backup_codes` json DEFAULT NULL,
  `two_factor_confirmed_at` timestamp NULL DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `user_oauth_accounts`
--

CREATE TABLE `user_oauth_accounts` (
  `id` bigint NOT NULL,
  `user_id` bigint NOT NULL,
  `provider` enum('google','github','apple') COLLATE utf8mb4_unicode_ci NOT NULL,
  `provider_user_id` varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
  `provider_email` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `provider_avatar` varchar(500) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `provider_name` varchar(255) COLLATE utf8mb4_unicode_ci DEFAULT NULL,
  `access_token` text COLLATE utf8mb4_unicode_ci,
  `refresh_token` text COLLATE utf8mb4_unicode_ci,
  `token_expires_at` datetime DEFAULT NULL,
  `linked_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- --------------------------------------------------------

--
-- Table structure for table `zones`
--

CREATE TABLE `zones` (
  `id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `classroom_id` varchar(21) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `name` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT 'ชื่อโซน เช่น โซน A, แถวหน้า',
  `x` int NOT NULL DEFAULT '0' COMMENT 'ตำแหน่ง X บน canvas',
  `y` int NOT NULL DEFAULT '0' COMMENT 'ตำแหน่ง Y บน canvas',
  `width` int NOT NULL DEFAULT '400' COMMENT 'ความกว้างโซน (px)',
  `height` int NOT NULL DEFAULT '300' COMMENT 'ความสูงโซน (px)',
  `color` varchar(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT 'rgba(99,102,241,0.15)' COMMENT 'สีโซน',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='โซนแบ่งพื้นที่บน Canvas ผังห้องเรียน';

--
-- Indexes for dumped tables
--

--
-- Indexes for table `assignments`
--
ALTER TABLE `assignments`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_assignments_course` (`course_id`),
  ADD KEY `idx_assignments_type` (`assignment_type`),
  ADD KEY `idx_assignments_week` (`week_number`),
  ADD KEY `idx_assignments_order` (`order_index`),
  ADD KEY `created_by` (`created_by`),
  ADD KEY `idx_assignments_linked_attendance` (`linked_attendance_session_id`),
  ADD KEY `idx_assignments_active` (`is_active`),
  ADD KEY `idx_assignments_linked_att` (`linked_attendance_session_id`),
  ADD KEY `idx_assignments_course_active` (`course_id`,`is_active`);

--
-- Indexes for table `assignment_attendance_links`
--
ALTER TABLE `assignment_attendance_links`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_assignment_attendance` (`assignment_id`,`attendance_session_id`),
  ADD KEY `idx_aal_assignment` (`assignment_id`),
  ADD KEY `idx_aal_attendance` (`attendance_session_id`);

--
-- Indexes for table `assignment_sub_items`
--
ALTER TABLE `assignment_sub_items`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_assignment_id` (`assignment_id`);

--
-- Indexes for table `attendance_records`
--
ALTER TABLE `attendance_records`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_att_unique` (`attendance_session_id`,`student_id`),
  ADD UNIQUE KEY `uk_attendance_session_student` (`attendance_session_id`,`student_id`),
  ADD KEY `fk_ar_student` (`student_id`),
  ADD KEY `fk_ar_updater` (`updated_by`),
  ADD KEY `idx_att_status` (`status`),
  ADD KEY `idx_att_records_session` (`attendance_session_id`),
  ADD KEY `idx_att_records_student` (`student_id`),
  ADD KEY `idx_att_records_status` (`status`),
  ADD KEY `idx_att_records_session_student` (`attendance_session_id`,`student_id`),
  ADD KEY `idx_att_records_session_status` (`attendance_session_id`,`status`);

--
-- Indexes for table `attendance_sessions`
--
ALTER TABLE `attendance_sessions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `fk_att_course` (`course_id`),
  ADD KEY `fk_att_section` (`course_section_id`),
  ADD KEY `fk_att_creator` (`created_by`),
  ADD KEY `idx_attsess_status` (`status`),
  ADD KEY `idx_attsess_start_time` (`start_time`),
  ADD KEY `idx_att_sessions_course` (`course_id`),
  ADD KEY `idx_att_sessions_section` (`course_section_id`),
  ADD KEY `idx_att_sessions_status` (`status`),
  ADD KEY `idx_att_sessions_start` (`start_time`),
  ADD KEY `idx_att_sessions_course_status` (`course_id`,`status`);

--
-- Indexes for table `attendance_session_sections`
--
ALTER TABLE `attendance_session_sections`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_session_section` (`attendance_session_id`,`course_section_id`),
  ADD KEY `course_section_id` (`course_section_id`);

--
-- Indexes for table `bonus_scores`
--
ALTER TABLE `bonus_scores`
  ADD PRIMARY KEY (`id`),
  ADD KEY `given_by` (`given_by`),
  ADD KEY `idx_bonus_scores_course` (`course_id`),
  ADD KEY `idx_bonus_scores_student` (`student_id`),
  ADD KEY `idx_bonus_scores_course_student` (`course_id`,`student_id`);

--
-- Indexes for table `classrooms`
--
ALTER TABLE `classrooms`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_classrooms_building` (`building`),
  ADD KEY `idx_classrooms_is_deleted` (`is_deleted`),
  ADD KEY `fk_classrooms_created_by` (`created_by`);

--
-- Indexes for table `courses`
--
ALTER TABLE `courses`
  ADD PRIMARY KEY (`id`),
  ADD KEY `fk_course_instructor` (`instructor_id`),
  ADD KEY `idx_courses_year_semester` (`year`,`semester`);

--
-- Indexes for table `course_activity_logs`
--
ALTER TABLE `course_activity_logs`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_cal_course_id` (`course_id`),
  ADD KEY `idx_cal_actor` (`actor_user_id`),
  ADD KEY `idx_cal_action` (`action`),
  ADD KEY `idx_cal_category` (`category`),
  ADD KEY `idx_cal_created_at` (`created_at`),
  ADD KEY `idx_cal_course_action` (`course_id`,`action`),
  ADD KEY `idx_cal_course_created` (`course_id`,`created_at`);

--
-- Indexes for table `course_instructors`
--
ALTER TABLE `course_instructors`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uk_course_instructor` (`course_id`,`user_id`),
  ADD UNIQUE KEY `uk_course_instructors_course_user` (`course_id`,`user_id`),
  ADD KEY `fk_ci_course` (`course_id`),
  ADD KEY `fk_ci_user` (`user_id`);

--
-- Indexes for table `course_sections`
--
ALTER TABLE `course_sections`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_course_section` (`course_id`,`section_no`);

--
-- Indexes for table `course_section_students`
--
ALTER TABLE `course_section_students`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_enroll` (`course_section_id`,`student_id`),
  ADD UNIQUE KEY `uk_css_section_student` (`course_section_id`,`student_id`),
  ADD KEY `fk_css_student` (`student_id`),
  ADD KEY `idx_css_section` (`course_section_id`),
  ADD KEY `idx_css_student` (`student_id`);

--
-- Indexes for table `course_tas`
--
ALTER TABLE `course_tas`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_course_ta` (`course_id`,`user_id`),
  ADD UNIQUE KEY `uk_course_tas_course_user` (`course_id`,`user_id`),
  ADD KEY `fk_ct_user` (`user_id`);

--
-- Indexes for table `desks`
--
ALTER TABLE `desks`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_desks_classroom` (`classroom_id`),
  ADD KEY `idx_desks_number` (`classroom_id`,`number`);

--
-- Indexes for table `exam_scores`
--
ALTER TABLE `exam_scores`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uk_exam_scores_setting_student` (`exam_setting_id`,`student_id`),
  ADD KEY `idx_exam_scores_student` (`student_id`),
  ADD KEY `idx_exam_scores_graded_by` (`graded_by`),
  ADD KEY `idx_exam_scores_setting_score` (`exam_setting_id`,`score`);

--
-- Indexes for table `exam_settings`
--
ALTER TABLE `exam_settings`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uk_exam_settings_course_type_component` (`course_id`,`exam_type`,`component`),
  ADD KEY `idx_exam_settings_course` (`course_id`);

--
-- Indexes for table `fcm_tokens`
--
ALTER TABLE `fcm_tokens`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_token` (`fcm_token`(255)),
  ADD KEY `idx_fcm_token` (`fcm_token`(255)),
  ADD KEY `idx_user_type` (`user_type`),
  ADD KEY `idx_user_id` (`user_id`),
  ADD KEY `idx_student_id` (`student_id`),
  ADD KEY `idx_booking_id` (`booking_id`),
  ADD KEY `idx_session_id` (`session_id`),
  ADD KEY `idx_is_active` (`is_active`);

--
-- Indexes for table `feedbacks`
--
ALTER TABLE `feedbacks`
  ADD PRIMARY KEY (`id`),
  ADD KEY `resolved_by` (`resolved_by`),
  ADD KEY `idx_feedback_status` (`status`),
  ADD KEY `idx_feedback_type` (`type`),
  ADD KEY `idx_feedback_priority` (`priority`),
  ADD KEY `idx_feedback_user` (`user_id`),
  ADD KEY `idx_feedback_created` (`created_at`);

--
-- Indexes for table `notification_logs`
--
ALTER TABLE `notification_logs`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_fcm_token_id` (`fcm_token_id`),
  ADD KEY `idx_notification_type` (`notification_type`),
  ADD KEY `idx_status` (`status`),
  ADD KEY `idx_created_at` (`created_at`);

--
-- Indexes for table `password_reset_tokens`
--
ALTER TABLE `password_reset_tokens`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `token` (`token`),
  ADD KEY `idx_token` (`token`),
  ADD KEY `idx_user_id` (`user_id`),
  ADD KEY `idx_expires_at` (`expires_at`);

--
-- Indexes for table `queue_bookings`
--
ALTER TABLE `queue_bookings`
  ADD PRIMARY KEY (`id`),
  ADD KEY `assigned_worker_id` (`assigned_worker_id`),
  ADD KEY `idx_queue_session` (`queue_session_id`),
  ADD KEY `idx_student_id` (`student_id`),
  ADD KEY `idx_desk_id` (`desk_id`),
  ADD KEY `idx_status` (`status`),
  ADD KEY `idx_booking_type` (`booking_type`),
  ADD KEY `idx_queue_number` (`queue_session_id`,`queue_number`),
  ADD KEY `idx_queue_bookings_session_student` (`queue_session_id`,`student_id`);

--
-- Indexes for table `queue_desk_status`
--
ALTER TABLE `queue_desk_status`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_desk_session` (`queue_session_id`,`desk_id`),
  ADD KEY `desk_id` (`desk_id`),
  ADD KEY `idx_queue_session` (`queue_session_id`),
  ADD KEY `idx_grading_status` (`grading_status`),
  ADD KEY `idx_help_status` (`help_status`);

--
-- Indexes for table `queue_sessions`
--
ALTER TABLE `queue_sessions`
  ADD PRIMARY KEY (`id`),
  ADD KEY `linked_assignment_id` (`linked_assignment_id`),
  ADD KEY `linked_attendance_session_id` (`linked_attendance_session_id`),
  ADD KEY `created_by` (`created_by`),
  ADD KEY `idx_course_id` (`course_id`),
  ADD KEY `idx_classroom_id` (`classroom_id`),
  ADD KEY `idx_status` (`status`),
  ADD KEY `idx_pin_code` (`pin_code`);

--
-- Indexes for table `queue_workers`
--
ALTER TABLE `queue_workers`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_worker` (`queue_session_id`,`user_id`),
  ADD KEY `idx_queue_session` (`queue_session_id`),
  ADD KEY `idx_user_id` (`user_id`),
  ADD KEY `idx_status` (`status`);

--
-- Indexes for table `refresh_tokens`
--
ALTER TABLE `refresh_tokens`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `jti` (`jti`),
  ADD KEY `fk_rt_user` (`user_id`);

--
-- Indexes for table `scores`
--
ALTER TABLE `scores`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_student_sub_item` (`assignment_id`,`student_id`,`sub_item_id`),
  ADD KEY `idx_scores_assignment` (`assignment_id`),
  ADD KEY `idx_scores_student` (`student_id`),
  ADD KEY `idx_scores_group` (`group_id`),
  ADD KEY `idx_scores_status` (`status`),
  ADD KEY `graded_by` (`graded_by`),
  ADD KEY `idx_scores_sub_item` (`sub_item_id`),
  ADD KEY `idx_scores_graded_by` (`graded_by`),
  ADD KEY `idx_scores_assignment_student` (`assignment_id`,`student_id`),
  ADD KEY `idx_scores_assignment_group` (`assignment_id`,`group_id`);

--
-- Indexes for table `score_edit_requests`
--
ALTER TABLE `score_edit_requests`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_edit_requests_score` (`score_id`),
  ADD KEY `idx_edit_requests_status` (`status`),
  ADD KEY `idx_edit_requests_requester` (`requested_by`),
  ADD KEY `reviewed_by` (`reviewed_by`);

--
-- Indexes for table `students`
--
ALTER TABLE `students`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `student_id` (`student_id`),
  ADD KEY `idx_students_is_active` (`is_active`);

--
-- Indexes for table `student_groups`
--
ALTER TABLE `student_groups`
  ADD PRIMARY KEY (`id`),
  ADD KEY `fk_sg2_course` (`course_id`),
  ADD KEY `idx_sg_course_type` (`course_id`,`group_type`),
  ADD KEY `idx_sg_week` (`week_number`);

--
-- Indexes for table `student_group_members`
--
ALTER TABLE `student_group_members`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_group_member` (`group_id`,`student_id`),
  ADD UNIQUE KEY `uk_sgm_group_student` (`group_id`,`student_id`),
  ADD KEY `fk_sgm_student` (`student_id`),
  ADD KEY `idx_sgm_group` (`group_id`),
  ADD KEY `idx_sgm_student` (`student_id`);

--
-- Indexes for table `system_logs`
--
ALTER TABLE `system_logs`
  ADD PRIMARY KEY (`id`),
  ADD KEY `fk_log_actor` (`actor_user_id`),
  ADD KEY `idx_syslog_action` (`action`),
  ADD KEY `idx_syslog_created_at` (`created_at`),
  ADD KEY `idx_syslog_type` (`log_type`),
  ADD KEY `idx_syslog_severity` (`severity`),
  ADD KEY `idx_syslog_method` (`http_method`),
  ADD KEY `idx_syslog_status_code` (`status_code`),
  ADD KEY `idx_syslog_user` (`actor_user_id`),
  ADD KEY `idx_syslog_session` (`session_id`),
  ADD KEY `idx_syslog_type_created` (`log_type`,`created_at`),
  ADD KEY `idx_syslog_severity_created` (`severity`,`created_at`),
  ADD KEY `system_logs_actor_user_id_created_at` (`actor_user_id`,`created_at`);

--
-- Indexes for table `two_factor_pending`
--
ALTER TABLE `two_factor_pending`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `unique_user_method` (`user_id`,`method`),
  ADD KEY `idx_two_factor_pending_expires` (`expires_at`);

--
-- Indexes for table `users`
--
ALTER TABLE `users`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `username` (`username`),
  ADD KEY `idx_users_role` (`role`),
  ADD KEY `idx_users_is_active` (`is_active`),
  ADD KEY `idx_users_two_factor_enabled` (`two_factor_enabled`);

--
-- Indexes for table `user_oauth_accounts`
--
ALTER TABLE `user_oauth_accounts`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uk_user_provider` (`user_id`,`provider`),
  ADD UNIQUE KEY `uk_provider_account` (`provider`,`provider_user_id`),
  ADD KEY `idx_user_id` (`user_id`),
  ADD KEY `idx_provider` (`provider`),
  ADD KEY `idx_provider_email` (`provider_email`);

--
-- Indexes for table `zones`
--
ALTER TABLE `zones`
  ADD PRIMARY KEY (`id`),
  ADD KEY `idx_zones_classroom_id` (`classroom_id`);

--
-- AUTO_INCREMENT for dumped tables
--

--
-- AUTO_INCREMENT for table `assignments`
--
ALTER TABLE `assignments`
  MODIFY `id` int NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `assignment_attendance_links`
--
ALTER TABLE `assignment_attendance_links`
  MODIFY `id` int NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `assignment_sub_items`
--
ALTER TABLE `assignment_sub_items`
  MODIFY `id` int NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `attendance_records`
--
ALTER TABLE `attendance_records`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `attendance_sessions`
--
ALTER TABLE `attendance_sessions`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `attendance_session_sections`
--
ALTER TABLE `attendance_session_sections`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `bonus_scores`
--
ALTER TABLE `bonus_scores`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `course_activity_logs`
--
ALTER TABLE `course_activity_logs`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `course_instructors`
--
ALTER TABLE `course_instructors`
  MODIFY `id` int NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `course_sections`
--
ALTER TABLE `course_sections`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `course_section_students`
--
ALTER TABLE `course_section_students`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `course_tas`
--
ALTER TABLE `course_tas`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `exam_scores`
--
ALTER TABLE `exam_scores`
  MODIFY `id` int NOT NULL AUTO_INCREMENT COMMENT 'Primary Key';

--
-- AUTO_INCREMENT for table `exam_settings`
--
ALTER TABLE `exam_settings`
  MODIFY `id` int NOT NULL AUTO_INCREMENT COMMENT 'Primary Key';

--
-- AUTO_INCREMENT for table `fcm_tokens`
--
ALTER TABLE `fcm_tokens`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `feedbacks`
--
ALTER TABLE `feedbacks`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `notification_logs`
--
ALTER TABLE `notification_logs`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `password_reset_tokens`
--
ALTER TABLE `password_reset_tokens`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `queue_bookings`
--
ALTER TABLE `queue_bookings`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `queue_desk_status`
--
ALTER TABLE `queue_desk_status`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `queue_workers`
--
ALTER TABLE `queue_workers`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `refresh_tokens`
--
ALTER TABLE `refresh_tokens`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `scores`
--
ALTER TABLE `scores`
  MODIFY `id` int NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `score_edit_requests`
--
ALTER TABLE `score_edit_requests`
  MODIFY `id` int NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `students`
--
ALTER TABLE `students`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `student_groups`
--
ALTER TABLE `student_groups`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `student_group_members`
--
ALTER TABLE `student_group_members`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `system_logs`
--
ALTER TABLE `system_logs`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `two_factor_pending`
--
ALTER TABLE `two_factor_pending`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `users`
--
ALTER TABLE `users`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- AUTO_INCREMENT for table `user_oauth_accounts`
--
ALTER TABLE `user_oauth_accounts`
  MODIFY `id` bigint NOT NULL AUTO_INCREMENT;

--
-- Constraints for dumped tables
--

--
-- Constraints for table `assignments`
--
ALTER TABLE `assignments`
  ADD CONSTRAINT `assignments_ibfk_1` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `assignments_ibfk_2` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE RESTRICT,
  ADD CONSTRAINT `fk_assignment_attendance_session` FOREIGN KEY (`linked_attendance_session_id`) REFERENCES `attendance_sessions` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `assignment_attendance_links`
--
ALTER TABLE `assignment_attendance_links`
  ADD CONSTRAINT `fk_aal_assignment` FOREIGN KEY (`assignment_id`) REFERENCES `assignments` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_aal_attendance` FOREIGN KEY (`attendance_session_id`) REFERENCES `attendance_sessions` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `assignment_sub_items`
--
ALTER TABLE `assignment_sub_items`
  ADD CONSTRAINT `assignment_sub_items_ibfk_1` FOREIGN KEY (`assignment_id`) REFERENCES `assignments` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `attendance_records`
--
ALTER TABLE `attendance_records`
  ADD CONSTRAINT `fk_ar_session` FOREIGN KEY (`attendance_session_id`) REFERENCES `attendance_sessions` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_ar_student` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_ar_updater` FOREIGN KEY (`updated_by`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `attendance_sessions`
--
ALTER TABLE `attendance_sessions`
  ADD CONSTRAINT `fk_att_course` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_att_creator` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE SET NULL,
  ADD CONSTRAINT `fk_att_section` FOREIGN KEY (`course_section_id`) REFERENCES `course_sections` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `attendance_session_sections`
--
ALTER TABLE `attendance_session_sections`
  ADD CONSTRAINT `attendance_session_sections_ibfk_1` FOREIGN KEY (`attendance_session_id`) REFERENCES `attendance_sessions` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `attendance_session_sections_ibfk_2` FOREIGN KEY (`course_section_id`) REFERENCES `course_sections` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `bonus_scores`
--
ALTER TABLE `bonus_scores`
  ADD CONSTRAINT `bonus_scores_ibfk_1` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `bonus_scores_ibfk_2` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `bonus_scores_ibfk_3` FOREIGN KEY (`given_by`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `classrooms`
--
ALTER TABLE `classrooms`
  ADD CONSTRAINT `fk_classrooms_created_by` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `courses`
--
ALTER TABLE `courses`
  ADD CONSTRAINT `fk_course_instructor` FOREIGN KEY (`instructor_id`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `course_instructors`
--
ALTER TABLE `course_instructors`
  ADD CONSTRAINT `fk_ci_course` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_ci_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `course_sections`
--
ALTER TABLE `course_sections`
  ADD CONSTRAINT `fk_cs_course` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `course_section_students`
--
ALTER TABLE `course_section_students`
  ADD CONSTRAINT `fk_css_section` FOREIGN KEY (`course_section_id`) REFERENCES `course_sections` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_css_student` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `course_tas`
--
ALTER TABLE `course_tas`
  ADD CONSTRAINT `fk_ct_course` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_ct_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `desks`
--
ALTER TABLE `desks`
  ADD CONSTRAINT `fk_desks_classroom` FOREIGN KEY (`classroom_id`) REFERENCES `classrooms` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `exam_scores`
--
ALTER TABLE `exam_scores`
  ADD CONSTRAINT `fk_exam_scores_graded_by` FOREIGN KEY (`graded_by`) REFERENCES `users` (`id`) ON DELETE SET NULL,
  ADD CONSTRAINT `fk_exam_scores_setting` FOREIGN KEY (`exam_setting_id`) REFERENCES `exam_settings` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_exam_scores_student` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `exam_settings`
--
ALTER TABLE `exam_settings`
  ADD CONSTRAINT `fk_exam_settings_course` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `fcm_tokens`
--
ALTER TABLE `fcm_tokens`
  ADD CONSTRAINT `fk_fcm_booking` FOREIGN KEY (`booking_id`) REFERENCES `queue_bookings` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_fcm_session` FOREIGN KEY (`session_id`) REFERENCES `queue_sessions` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_fcm_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `feedbacks`
--
ALTER TABLE `feedbacks`
  ADD CONSTRAINT `feedbacks_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE SET NULL,
  ADD CONSTRAINT `feedbacks_ibfk_2` FOREIGN KEY (`resolved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `notification_logs`
--
ALTER TABLE `notification_logs`
  ADD CONSTRAINT `fk_notification_fcm_token` FOREIGN KEY (`fcm_token_id`) REFERENCES `fcm_tokens` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `password_reset_tokens`
--
ALTER TABLE `password_reset_tokens`
  ADD CONSTRAINT `password_reset_tokens_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `queue_bookings`
--
ALTER TABLE `queue_bookings`
  ADD CONSTRAINT `queue_bookings_ibfk_1` FOREIGN KEY (`queue_session_id`) REFERENCES `queue_sessions` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_bookings_ibfk_2` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_bookings_ibfk_3` FOREIGN KEY (`desk_id`) REFERENCES `desks` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_bookings_ibfk_4` FOREIGN KEY (`assigned_worker_id`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `queue_desk_status`
--
ALTER TABLE `queue_desk_status`
  ADD CONSTRAINT `queue_desk_status_ibfk_1` FOREIGN KEY (`queue_session_id`) REFERENCES `queue_sessions` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_desk_status_ibfk_2` FOREIGN KEY (`desk_id`) REFERENCES `desks` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `queue_sessions`
--
ALTER TABLE `queue_sessions`
  ADD CONSTRAINT `queue_sessions_ibfk_1` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_sessions_ibfk_2` FOREIGN KEY (`classroom_id`) REFERENCES `classrooms` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_sessions_ibfk_3` FOREIGN KEY (`linked_assignment_id`) REFERENCES `assignments` (`id`) ON DELETE SET NULL,
  ADD CONSTRAINT `queue_sessions_ibfk_4` FOREIGN KEY (`linked_attendance_session_id`) REFERENCES `attendance_sessions` (`id`) ON DELETE SET NULL,
  ADD CONSTRAINT `queue_sessions_ibfk_5` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `queue_workers`
--
ALTER TABLE `queue_workers`
  ADD CONSTRAINT `queue_workers_ibfk_1` FOREIGN KEY (`queue_session_id`) REFERENCES `queue_sessions` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `queue_workers_ibfk_2` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `refresh_tokens`
--
ALTER TABLE `refresh_tokens`
  ADD CONSTRAINT `fk_rt_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `scores`
--
ALTER TABLE `scores`
  ADD CONSTRAINT `fk_scores_sub_item` FOREIGN KEY (`sub_item_id`) REFERENCES `assignment_sub_items` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `scores_ibfk_1` FOREIGN KEY (`assignment_id`) REFERENCES `assignments` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `scores_ibfk_2` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `scores_ibfk_3` FOREIGN KEY (`group_id`) REFERENCES `student_groups` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `scores_ibfk_4` FOREIGN KEY (`graded_by`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `score_edit_requests`
--
ALTER TABLE `score_edit_requests`
  ADD CONSTRAINT `score_edit_requests_ibfk_1` FOREIGN KEY (`score_id`) REFERENCES `scores` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `score_edit_requests_ibfk_2` FOREIGN KEY (`requested_by`) REFERENCES `users` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `score_edit_requests_ibfk_3` FOREIGN KEY (`reviewed_by`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `student_groups`
--
ALTER TABLE `student_groups`
  ADD CONSTRAINT `fk_sg2_course` FOREIGN KEY (`course_id`) REFERENCES `courses` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `student_group_members`
--
ALTER TABLE `student_group_members`
  ADD CONSTRAINT `fk_sgm_group` FOREIGN KEY (`group_id`) REFERENCES `student_groups` (`id`) ON DELETE CASCADE,
  ADD CONSTRAINT `fk_sgm_student` FOREIGN KEY (`student_id`) REFERENCES `students` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `system_logs`
--
ALTER TABLE `system_logs`
  ADD CONSTRAINT `fk_log_actor` FOREIGN KEY (`actor_user_id`) REFERENCES `users` (`id`) ON DELETE SET NULL;

--
-- Constraints for table `two_factor_pending`
--
ALTER TABLE `two_factor_pending`
  ADD CONSTRAINT `fk_two_factor_pending_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `user_oauth_accounts`
--
ALTER TABLE `user_oauth_accounts`
  ADD CONSTRAINT `fk_user_oauth_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;

--
-- Constraints for table `zones`
--
ALTER TABLE `zones`
  ADD CONSTRAINT `fk_zones_classroom` FOREIGN KEY (`classroom_id`) REFERENCES `classrooms` (`id`) ON DELETE CASCADE ON UPDATE CASCADE;
COMMIT;

/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;

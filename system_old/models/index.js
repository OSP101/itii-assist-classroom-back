const { sequelize } = require('../config/database');
const User = require('./User');
const Student = require('./Student');
const RefreshToken = require('./RefreshToken');
const SystemLog = require('./SystemLog');
const Course = require('./Course');
const CourseSection = require('./CourseSection');
const CourseInstructor = require('./CourseInstructor');
const CourseTA = require('./CourseTA');
const CourseSectionStudent = require('./CourseSectionStudent');
const Classroom = require('./Classroom');
const Desk = require('./Desk');
const Zone = require('./Zone');
const Feedback = require('./Feedback');
const StudentGroup = require('./StudentGroup');
const StudentGroupMember = require('./StudentGroupMember');
const Assignment = require('./Assignment');
const AssignmentSubItem = require('./AssignmentSubItem');
const AssignmentAttendanceLink = require('./AssignmentAttendanceLink');
const Score = require('./Score');
const ScoreEditRequest = require('./ScoreEditRequest');
const AttendanceSession = require('./AttendanceSession');
const AttendanceSessionSection = require('./AttendanceSessionSection');
const AttendanceRecord = require('./AttendanceRecord');
const BonusScore = require('./BonusScore');

// Queue System Models
const QueueSession = require('./QueueSession');
const QueueWorker = require('./QueueWorker');
const QueueBooking = require('./QueueBooking');
const QueueDeskStatus = require('./QueueDeskStatus');

// FCM Notification Models
const FcmToken = require('./FcmToken');
const NotificationLog = require('./NotificationLog');

// Activity Log
const CourseActivityLog = require('./CourseActivityLog');

// Exam Score Models
const ExamSetting = require('./ExamSetting');
const ExamScore = require('./ExamScore');

// Two-Factor Authentication
const TwoFactorPending = require('./TwoFactorPending');

// OAuth Accounts
const UserOAuthAccount = require('./UserOAuthAccount');

// Password Reset
const PasswordResetToken = require('./PasswordResetToken');

// ============================================
// Define Associations
// ============================================;

// User -> RefreshToken
User.hasMany(RefreshToken, {
  foreignKey: 'user_id',
  as: 'refreshTokens',
});

RefreshToken.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

// User -> SystemLog
User.hasMany(SystemLog, {
  foreignKey: 'actor_user_id',
  as: 'logs',
});

SystemLog.belongsTo(User, {
  foreignKey: 'actor_user_id',
  as: 'actor',
});

// ============================================
// Course Associations
// ============================================

// Course -> Instructor (User)
Course.belongsTo(User, {
  foreignKey: 'instructor_id',
  as: 'instructor',
});

User.hasMany(Course, {
  foreignKey: 'instructor_id',
  as: 'instructorCourses',
});

// Course -> Multiple Instructors (through CourseInstructor)
Course.belongsToMany(User, {
  through: CourseInstructor,
  foreignKey: 'course_id',
  otherKey: 'user_id',
  as: 'instructors',
});

User.belongsToMany(Course, {
  through: CourseInstructor,
  foreignKey: 'user_id',
  otherKey: 'course_id',
  as: 'instructingCourses',
});

CourseInstructor.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

CourseInstructor.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

// Course -> Sections
Course.hasMany(CourseSection, {
  foreignKey: 'course_id',
  as: 'sections',
});

CourseSection.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

// Course -> TAs (through CourseTA)
Course.belongsToMany(User, {
  through: CourseTA,
  foreignKey: 'course_id',
  otherKey: 'user_id',
  as: 'tas',
});

User.belongsToMany(Course, {
  through: CourseTA,
  foreignKey: 'user_id',
  otherKey: 'course_id',
  as: 'taCourses',
});

CourseTA.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

CourseTA.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'taUser',
});

// CourseSection -> Students (through CourseSectionStudent)
CourseSection.belongsToMany(Student, {
  through: CourseSectionStudent,
  foreignKey: 'course_section_id',
  otherKey: 'student_id',
  as: 'students',
});

Student.belongsToMany(CourseSection, {
  through: CourseSectionStudent,
  foreignKey: 'student_id',
  otherKey: 'course_section_id',
  as: 'sections',
});

CourseSectionStudent.belongsTo(CourseSection, {
  foreignKey: 'course_section_id',
  as: 'section',
});

CourseSectionStudent.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

// ============================================
// Classroom Associations
// ============================================

// Classroom -> User (created_by)
Classroom.belongsTo(User, {
  foreignKey: 'created_by',
  as: 'creator',
});

User.hasMany(Classroom, {
  foreignKey: 'created_by',
  as: 'createdClassrooms',
});

// Classroom -> Desks
Classroom.hasMany(Desk, {
  foreignKey: 'classroom_id',
  as: 'desks',
});

Desk.belongsTo(Classroom, {
  foreignKey: 'classroom_id',
  as: 'classroom',
});

// Classroom -> Zones
Classroom.hasMany(Zone, {
  foreignKey: 'classroom_id',
  as: 'zones',
});

Zone.belongsTo(Classroom, {
  foreignKey: 'classroom_id',
  as: 'classroom',
});

// CourseActivityLog -> User, Course
CourseActivityLog.belongsTo(User, {
  foreignKey: 'actor_user_id',
  as: 'actor',
});

CourseActivityLog.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

// Feedback -> User
Feedback.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

Feedback.belongsTo(User, {
  foreignKey: 'resolved_by',
  as: 'resolver',
});

User.hasMany(Feedback, {
  foreignKey: 'user_id',
  as: 'feedbacks',
});

// ============================================
// Student Group Associations
// ============================================

// StudentGroup -> Course
StudentGroup.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

Course.hasMany(StudentGroup, {
  foreignKey: 'course_id',
  as: 'studentGroups',
});

// StudentGroup -> Members (through StudentGroupMember)
StudentGroup.belongsToMany(Student, {
  through: StudentGroupMember,
  foreignKey: 'group_id',
  otherKey: 'student_id',
  as: 'members',
});

Student.belongsToMany(StudentGroup, {
  through: StudentGroupMember,
  foreignKey: 'student_id',
  otherKey: 'group_id',
  as: 'groups',
});

StudentGroupMember.belongsTo(StudentGroup, {
  foreignKey: 'group_id',
  as: 'group',
});

StudentGroupMember.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

// ============================================
// Assignment & Score Associations
// ============================================

// Assignment -> Course
Assignment.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

Course.hasMany(Assignment, {
  foreignKey: 'course_id',
  as: 'assignments',
});

// Assignment -> Creator (User)
Assignment.belongsTo(User, {
  foreignKey: 'created_by',
  as: 'creator',
});

// Assignment -> SubItems
Assignment.hasMany(AssignmentSubItem, {
  foreignKey: 'assignment_id',
  as: 'subItems',
});

AssignmentSubItem.belongsTo(Assignment, {
  foreignKey: 'assignment_id',
  as: 'assignment',
});

// Assignment -> AttendanceSession (optional link - legacy, kept for backward compatibility)
Assignment.belongsTo(AttendanceSession, {
  foreignKey: 'linked_attendance_session_id',
  as: 'linkedAttendanceSession',
});

AttendanceSession.hasMany(Assignment, {
  foreignKey: 'linked_attendance_session_id',
  as: 'linkedAssignments',
});

// Assignment <-> AttendanceSession (many-to-many through AssignmentAttendanceLink)
Assignment.belongsToMany(AttendanceSession, {
  through: AssignmentAttendanceLink,
  foreignKey: 'assignment_id',
  otherKey: 'attendance_session_id',
  as: 'linkedAttendanceSessions', // Note: plural for new many-to-many
});

AttendanceSession.belongsToMany(Assignment, {
  through: AssignmentAttendanceLink,
  foreignKey: 'attendance_session_id',
  otherKey: 'assignment_id',
  as: 'linkedAssignmentsMany',
});

// AssignmentAttendanceLink associations
AssignmentAttendanceLink.belongsTo(Assignment, {
  foreignKey: 'assignment_id',
  as: 'assignment',
});

AssignmentAttendanceLink.belongsTo(AttendanceSession, {
  foreignKey: 'attendance_session_id',
  as: 'attendanceSession',
});

// Score -> SubItem (optional)
Score.belongsTo(AssignmentSubItem, {
  foreignKey: 'sub_item_id',
  as: 'subItem',
});

AssignmentSubItem.hasMany(Score, {
  foreignKey: 'sub_item_id',
  as: 'scores',
});

// Score -> Assignment
Score.belongsTo(Assignment, {
  foreignKey: 'assignment_id',
  as: 'assignment',
});

Assignment.hasMany(Score, {
  foreignKey: 'assignment_id',
  as: 'scores',
});

// Score -> Student
Score.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

Student.hasMany(Score, {
  foreignKey: 'student_id',
  as: 'scores',
});

// Score -> Group (optional)
Score.belongsTo(StudentGroup, {
  foreignKey: 'group_id',
  as: 'group',
});

// Score -> Grader (User)
Score.belongsTo(User, {
  foreignKey: 'graded_by',
  as: 'grader',
});

// ScoreEditRequest -> Score
ScoreEditRequest.belongsTo(Score, {
  foreignKey: 'score_id',
  as: 'score',
});

Score.hasMany(ScoreEditRequest, {
  foreignKey: 'score_id',
  as: 'editRequests',
});

// ScoreEditRequest -> Requester (User)
ScoreEditRequest.belongsTo(User, {
  foreignKey: 'requested_by',
  as: 'requester',
});

// ScoreEditRequest -> Reviewer (User)
ScoreEditRequest.belongsTo(User, {
  foreignKey: 'reviewed_by',
  as: 'reviewer',
});

// ============================================
// Attendance Associations
// ============================================

// AttendanceSession -> Course
AttendanceSession.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

Course.hasMany(AttendanceSession, {
  foreignKey: 'course_id',
  as: 'attendanceSessions',
});

// AttendanceSession -> CourseSection (legacy one-to-one, kept for backward compatibility)
AttendanceSession.belongsTo(CourseSection, {
  foreignKey: 'course_section_id',
  as: 'section',
});

CourseSection.hasMany(AttendanceSession, {
  foreignKey: 'course_section_id',
  as: 'attendanceSessions',
});

// AttendanceSession <-> CourseSection (many-to-many through AttendanceSessionSection)
AttendanceSession.belongsToMany(CourseSection, {
  through: AttendanceSessionSection,
  foreignKey: 'attendance_session_id',
  otherKey: 'course_section_id',
  as: 'sections', // Plural: multiple sections per session
});

CourseSection.belongsToMany(AttendanceSession, {
  through: AttendanceSessionSection,
  foreignKey: 'course_section_id',
  otherKey: 'attendance_session_id',
  as: 'linkedAttendanceSessions',
});

// AttendanceSessionSection -> AttendanceSession
AttendanceSessionSection.belongsTo(AttendanceSession, {
  foreignKey: 'attendance_session_id',
  as: 'session',
});

// AttendanceSessionSection -> CourseSection
AttendanceSessionSection.belongsTo(CourseSection, {
  foreignKey: 'course_section_id',
  as: 'section',
});

// AttendanceSession -> Creator (User)
AttendanceSession.belongsTo(User, {
  foreignKey: 'created_by',
  as: 'creator',
});

// AttendanceSession -> Records
AttendanceSession.hasMany(AttendanceRecord, {
  foreignKey: 'attendance_session_id',
  as: 'records',
});

AttendanceRecord.belongsTo(AttendanceSession, {
  foreignKey: 'attendance_session_id',
  as: 'session',
});

// AttendanceRecord -> Student
AttendanceRecord.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

Student.hasMany(AttendanceRecord, {
  foreignKey: 'student_id',
  as: 'attendanceRecords',
});

// AttendanceRecord -> UpdatedBy (User)
AttendanceRecord.belongsTo(User, {
  foreignKey: 'updated_by',
  as: 'updater',
});

// ============================================
// Bonus Score Associations
// ============================================

// BonusScore -> Course
BonusScore.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

Course.hasMany(BonusScore, {
  foreignKey: 'course_id',
  as: 'bonusScores',
});

// BonusScore -> Student
BonusScore.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

Student.hasMany(BonusScore, {
  foreignKey: 'student_id',
  as: 'bonusScores',
});

// BonusScore -> User (given_by)
BonusScore.belongsTo(User, {
  foreignKey: 'given_by',
  as: 'giver',
});

User.hasMany(BonusScore, {
  foreignKey: 'given_by',
  as: 'givenBonusScores',
});

// ============================================
// Queue System Associations
// ============================================

// QueueSession -> Course
QueueSession.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

Course.hasMany(QueueSession, {
  foreignKey: 'course_id',
  as: 'queueSessions',
});

// QueueSession -> Classroom
QueueSession.belongsTo(Classroom, {
  foreignKey: 'classroom_id',
  as: 'classroom',
});

Classroom.hasMany(QueueSession, {
  foreignKey: 'classroom_id',
  as: 'queueSessions',
});

// QueueSession -> Assignment (linked)
QueueSession.belongsTo(Assignment, {
  foreignKey: 'linked_assignment_id',
  as: 'linkedAssignment',
});

Assignment.hasMany(QueueSession, {
  foreignKey: 'linked_assignment_id',
  as: 'queueSessions',
});

// QueueSession -> AttendanceSession (linked)
QueueSession.belongsTo(AttendanceSession, {
  foreignKey: 'linked_attendance_session_id',
  as: 'linkedAttendanceSession',
});

AttendanceSession.hasMany(QueueSession, {
  foreignKey: 'linked_attendance_session_id',
  as: 'queueSessions',
});

// QueueSession -> User (creator)
QueueSession.belongsTo(User, {
  foreignKey: 'created_by',
  as: 'creator',
});

User.hasMany(QueueSession, {
  foreignKey: 'created_by',
  as: 'createdQueueSessions',
});

// QueueWorker -> QueueSession
QueueWorker.belongsTo(QueueSession, {
  foreignKey: 'queue_session_id',
  as: 'session',
});

QueueSession.hasMany(QueueWorker, {
  foreignKey: 'queue_session_id',
  as: 'workers',
});

// QueueWorker -> User
QueueWorker.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

User.hasMany(QueueWorker, {
  foreignKey: 'user_id',
  as: 'queueWorkerAssignments',
});

// QueueBooking -> QueueSession
QueueBooking.belongsTo(QueueSession, {
  foreignKey: 'queue_session_id',
  as: 'session',
});

QueueSession.hasMany(QueueBooking, {
  foreignKey: 'queue_session_id',
  as: 'bookings',
});

// QueueBooking -> Student
QueueBooking.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

Student.hasMany(QueueBooking, {
  foreignKey: 'student_id',
  as: 'queueBookings',
});

// QueueBooking -> Desk
QueueBooking.belongsTo(Desk, {
  foreignKey: 'desk_id',
  as: 'desk',
});

Desk.hasMany(QueueBooking, {
  foreignKey: 'desk_id',
  as: 'queueBookings',
});

// QueueBooking -> User (assigned worker)
QueueBooking.belongsTo(User, {
  foreignKey: 'assigned_worker_id',
  as: 'assignedWorker',
});

User.hasMany(QueueBooking, {
  foreignKey: 'assigned_worker_id',
  as: 'assignedBookings',
});

// QueueDeskStatus -> QueueSession
QueueDeskStatus.belongsTo(QueueSession, {
  foreignKey: 'queue_session_id',
  as: 'session',
});

QueueSession.hasMany(QueueDeskStatus, {
  foreignKey: 'queue_session_id',
  as: 'deskStatuses',
});

// QueueDeskStatus -> Desk
QueueDeskStatus.belongsTo(Desk, {
  foreignKey: 'desk_id',
  as: 'desk',
});

Desk.hasMany(QueueDeskStatus, {
  foreignKey: 'desk_id',
  as: 'queueStatuses',
});

// ============================================
// FCM Token Associations
// ============================================

// FcmToken -> User (for workers)
FcmToken.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

User.hasMany(FcmToken, {
  foreignKey: 'user_id',
  as: 'fcmTokens',
});

// FcmToken -> QueueSession (for workers)
FcmToken.belongsTo(QueueSession, {
  foreignKey: 'session_id',
  as: 'queueSession',
});

QueueSession.hasMany(FcmToken, {
  foreignKey: 'session_id',
  as: 'fcmTokens',
});

// FcmToken -> QueueBooking (for students)
FcmToken.belongsTo(QueueBooking, {
  foreignKey: 'booking_id',
  as: 'booking',
});

QueueBooking.hasMany(FcmToken, {
  foreignKey: 'booking_id',
  as: 'fcmTokens',
});

// NotificationLog -> FcmToken
NotificationLog.belongsTo(FcmToken, {
  foreignKey: 'fcm_token_id',
  as: 'fcmToken',
});

FcmToken.hasMany(NotificationLog, {
  foreignKey: 'fcm_token_id',
  as: 'notificationLogs',
});

// ============================================
// Exam Score Associations
// ============================================

// Course -> ExamSetting
Course.hasMany(ExamSetting, {
  foreignKey: 'course_id',
  as: 'examSettings',
});

ExamSetting.belongsTo(Course, {
  foreignKey: 'course_id',
  as: 'course',
});

// ExamSetting -> ExamScore
ExamSetting.hasMany(ExamScore, {
  foreignKey: 'exam_setting_id',
  as: 'scores',
});

ExamScore.belongsTo(ExamSetting, {
  foreignKey: 'exam_setting_id',
  as: 'examSetting',
});

// Student -> ExamScore
Student.hasMany(ExamScore, {
  foreignKey: 'student_id',
  as: 'examScores',
});

ExamScore.belongsTo(Student, {
  foreignKey: 'student_id',
  as: 'student',
});

// User (grader) -> ExamScore
User.hasMany(ExamScore, {
  foreignKey: 'graded_by',
  as: 'gradedExamScores',
});

ExamScore.belongsTo(User, {
  foreignKey: 'graded_by',
  as: 'grader',
});

// ============================================
// User OAuth Accounts
// ============================================

// User -> OAuth Accounts
User.hasMany(UserOAuthAccount, {
  foreignKey: 'user_id',
  as: 'oauthAccounts',
  onDelete: 'CASCADE',
});

UserOAuthAccount.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

// ============================================
// Password Reset Tokens
// ============================================

// User -> Password Reset Tokens
User.hasMany(PasswordResetToken, {
  foreignKey: 'user_id',
  as: 'passwordResetTokens',
  onDelete: 'CASCADE',
});

PasswordResetToken.belongsTo(User, {
  foreignKey: 'user_id',
  as: 'user',
});

// ============================================
// Export all models
// ============================================
module.exports = {
  sequelize,
  User,
  Student,
  RefreshToken,
  SystemLog,
  Course,
  CourseSection,
  CourseInstructor,
  CourseTA,
  CourseSectionStudent,
  Classroom,
  Desk,
  Zone,
  Feedback,
  StudentGroup,
  StudentGroupMember,
  Assignment,
  AssignmentSubItem,
  AssignmentAttendanceLink,
  Score,
  ScoreEditRequest,
  AttendanceSession,
  AttendanceSessionSection,
  AttendanceRecord,
  BonusScore,
  // Queue System Models
  QueueSession,
  QueueWorker,
  QueueBooking,
  QueueDeskStatus,
  // FCM Notification Models
  FcmToken,
  NotificationLog,
  // Activity Log
  CourseActivityLog,
  // Exam Score Models
  ExamSetting,
  ExamScore,
  // Two-Factor Authentication
  TwoFactorPending,
  // OAuth Accounts
  UserOAuthAccount,
  // Password Reset
  PasswordResetToken,
};

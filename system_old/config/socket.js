/**
 * Socket.io Configuration
 * Real-time communication for attendance system and data sync
 */

const { Server } = require('socket.io');
const config = require('./index');

let io = null;

/**
 * Initialize Socket.io with HTTP server
 */
const initializeSocket = (httpServer) => {
io = new Server(httpServer, {
  path: "/socket.io",
  cors: {
    origin: (origin, callback) => {
      // Allow requests with no origin (like mobile apps, curl, or same-origin)
      if (!origin) return callback(null, true);
      
      const allowedOrigins = [
        config.frontendUrl,
        "http://localhost:3000",
        "http://localhost:3010",
        "http://127.0.0.1:3000",
        "http://10.199.10.10:3000",
        "https://itii.osp101.dev",
        "https://itii-mid.osp101.com",
      ];
      
      // Check if origin is in allowed list
      if (allowedOrigins.includes(origin)) {
        return callback(null, true);
      }
      
      // Allow local network IPs (192.168.x.x, 10.x.x.x, etc.)
      const localNetworkPattern = /^http:\/\/(192\.168\.\d+\.\d+|10\.\d+\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+\.\d+|localhost|127\.0\.0\.1)(:\d+)?$/;
      if (localNetworkPattern.test(origin)) {
        return callback(null, true);
      }
      
      console.log(`⚠️ Socket.IO CORS rejected origin: ${origin}`);
      callback(new Error('Not allowed by CORS'));
    },
    methods: ["GET", "POST"],
    credentials: true,
  },
  pingTimeout: 60000,
  pingInterval: 25000,
  transports: ["polling", "websocket"],
});


  // Connection handler
  io.on('connection', (socket) => {
    console.log(`🔌 Socket connected: ${socket.id}`);

    // ========== Attendance Rooms ==========
    // Join attendance room
    socket.on('join-attendance', (sessionId) => {
      const room = `attendance-${sessionId}`;
      socket.join(room);
      console.log(`👤 Socket ${socket.id} joined room: ${room}`);
    });

    // Leave attendance room
    socket.on('leave-attendance', (sessionId) => {
      const room = `attendance-${sessionId}`;
      socket.leave(room);
      console.log(`👤 Socket ${socket.id} left room: ${room}`);
    });

    // Instructor room (for receiving updates)
    socket.on('join-instructor', (sessionId) => {
      const room = `instructor-${sessionId}`;
      socket.join(room);
      console.log(`🎓 Instructor ${socket.id} joined room: ${room}`);
    });

    // Leave instructor room
    socket.on('leave-instructor', (sessionId) => {
      const room = `instructor-${sessionId}`;
      socket.leave(room);
      console.log(`🎓 Instructor ${socket.id} left room: ${room}`);
    });

    // ========== Course Sync Rooms ==========
    // Join user's course updates room
    socket.on('join-user-courses', (userId) => {
      const room = `user-courses-${userId}`;
      socket.join(room);
      // Also join global course updates room
      socket.join('global-courses');
      console.log(`📚 Socket ${socket.id} joined course updates room: ${room}`);
    });

    // Leave user's course updates room
    socket.on('leave-user-courses', (userId) => {
      const room = `user-courses-${userId}`;
      socket.leave(room);
      socket.leave('global-courses');
      console.log(`📚 Socket ${socket.id} left course updates room: ${room}`);
    });

    // Handle course change event - broadcast to all connected clients
    socket.on('course-change', (data) => {
      console.log(`📢 Course change event:`, data);
      // Broadcast to all clients in global-courses room (except sender)
      socket.to('global-courses').emit('course-updated', {
        ...data,
        timestamp: Date.now(),
      });
    });

    // ========== Classroom Sync Rooms ==========
    // Join classroom room for real-time updates
    socket.on('join-classroom', (classroomId) => {
      const room = `classroom-${classroomId}`;
      socket.join(room);
      console.log(`🏫 Socket ${socket.id} joined classroom room: ${room}`);
    });

    // Leave classroom room
    socket.on('leave-classroom', (classroomId) => {
      const room = `classroom-${classroomId}`;
      socket.leave(room);
      console.log(`🏫 Socket ${socket.id} left classroom room: ${room}`);
    });

    // Handle classroom data change
    socket.on('classroom-change', (data) => {
      const { classroomId, type, payload } = data;
      console.log(`📢 Classroom ${classroomId} change:`, type);
      // Broadcast to all clients in the classroom room (except sender)
      socket.to(`classroom-${classroomId}`).emit('classroom-updated', {
        type,
        payload,
        timestamp: Date.now(),
      });
    });

    // ========== Global Updates Room ==========
    // Join global updates room (for all resources)
    socket.on('join-global-updates', () => {
      socket.join('global-updates');
      console.log(`🌐 Socket ${socket.id} joined global updates room`);
    });

    // Leave global updates room
    socket.on('leave-global-updates', () => {
      socket.leave('global-updates');
      console.log(`🌐 Socket ${socket.id} left global updates room`);
    });

    // ========== Queue System Rooms ==========
    // Join queue session room (for students and instructors)
    socket.on('join-queue', (sessionId) => {
      const room = `queue-${sessionId}`;
      socket.join(room);
      console.log(`📋 Socket ${socket.id} joined queue room: ${room}`);
    });

    // Leave queue session room
    socket.on('leave-queue', (sessionId) => {
      const room = `queue-${sessionId}`;
      socket.leave(room);
      console.log(`📋 Socket ${socket.id} left queue room: ${room}`);
    });

    // Join worker room (for receiving new tasks)
    socket.on('join-worker', (userId) => {
      // Ensure userId is string for consistent room naming
      const room = `worker-${String(userId)}`;
      socket.join(room);
      console.log(`👷 Worker ${socket.id} joined room: ${room}`);
      // Also store the userId on socket for debugging
      socket.userId = String(userId);
    });

    // Leave worker room
    socket.on('leave-worker', (userId) => {
      const room = `worker-${String(userId)}`;
      socket.leave(room);
      console.log(`👷 Worker ${socket.id} left room: ${room}`);
    });

    // Join booking room (for students to receive updates on their booking)
    socket.on('join-booking', (bookingId) => {
      const room = `booking-${bookingId}`;
      socket.join(room);
      console.log(`🎫 Socket ${socket.id} joined booking room: ${room}`);
    });

    // Leave booking room
    socket.on('leave-booking', (bookingId) => {
      const room = `booking-${bookingId}`;
      socket.leave(room);
      console.log(`🎫 Socket ${socket.id} left booking room: ${room}`);
    });

    // ========== Generic Data Change Event ==========
    // Handle any data change and broadcast to all clients
    socket.on('data-change', (data) => {
      const { resource, action, id, data: payload } = data;
      console.log(`📢 Data change event - Resource: ${resource}, Action: ${action}, ID: ${id || 'N/A'}`);
      
      // Broadcast to all clients in global-updates room (except sender)
      socket.to('global-updates').emit('data-updated', {
        resource,
        action,
        id,
        data: payload,
        timestamp: Date.now(),
      });

      // Also broadcast to global-courses room for backward compatibility
      if (resource === 'course') {
        socket.to('global-courses').emit('course-updated', {
          action,
          courseId: id,
          timestamp: Date.now(),
        });
      }
    });

    // Handle disconnection
    socket.on('disconnect', (reason) => {
      console.log(`🔌 Socket disconnected: ${socket.id}, reason: ${reason}`);
    });
  });

  return io;
};

/**
 * Get Socket.io instance
 */
const getIO = () => {
  if (!io) {
    throw new Error('Socket.io not initialized. Call initializeSocket first.');
  }
  return io;
};

/**
 * Emit to attendance room
 */
const emitToAttendance = (sessionId, event, data) => {
  if (io) {
    io.to(`attendance-${sessionId}`).emit(event, data);
    io.to(`instructor-${sessionId}`).emit(event, data);
  }
};

/**
 * Emit to instructor room only
 */
const emitToInstructor = (sessionId, event, data) => {
  if (io) {
    io.to(`instructor-${sessionId}`).emit(event, data);
  }
};

/**
 * Emit course update to all connected users
 */
const emitCourseUpdate = (action, courseId, userId) => {
  if (io) {
    io.to('global-courses').emit('course-updated', {
      action,
      courseId,
      userId,
      timestamp: Date.now(),
    });
  }
};

/**
 * Emit classroom update
 */
const emitToClassroom = (classroomId, type, payload) => {
  if (io) {
    io.to(`classroom-${classroomId}`).emit('classroom-updated', {
      type,
      payload,
      timestamp: Date.now(),
    });
  }
};

/**
 * Emit generic data update to all connected clients
 * @param {string} resource - Resource type (course, student, user, classroom, etc.)
 * @param {string} action - Action type (create, update, delete, toggle, bulk)
 * @param {string|number} id - Optional resource ID
 * @param {any} data - Optional additional data
 */
const emitDataUpdate = (resource, action, id = null, data = null) => {
  if (io) {
    io.to('global-updates').emit('data-updated', {
      resource,
      action,
      id,
      data,
      timestamp: Date.now(),
    });
    console.log(`📢 Data update emitted - Resource: ${resource}, Action: ${action}, ID: ${id || 'N/A'}`);
  }
};

module.exports = {
  initializeSocket,
  getIO,
  // Attendance
  emitToAttendance,
  emitToInstructor,
  emitCourseUpdate,
  emitToClassroom,
  emitDataUpdate,
};

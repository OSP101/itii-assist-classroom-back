const multer = require('multer');
const path = require('path');
const fs = require('fs');
const sharp = require('sharp');

// Ensure upload directory exists
const uploadDir = path.join(__dirname, '../../uploads/score-edit-requests');
if (!fs.existsSync(uploadDir)) {
    fs.mkdirSync(uploadDir, { recursive: true });
}

// Use memory storage for image processing
const memoryStorage = multer.memoryStorage();

// File filter - only allow images
const fileFilter = (req, file, cb) => {
    const allowedTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/gif', 'image/webp'];
    if (allowedTypes.includes(file.mimetype)) {
        cb(null, true);
    } else {
        cb(new Error('Only image files are allowed (jpeg, jpg, png, gif, webp)'), false);
    }
};

// Create multer instance with memory storage
const uploadScoreEditImages = multer({
    storage: memoryStorage,
    fileFilter,
    limits: {
        fileSize: 10 * 1024 * 1024, // 10MB max per file (before compression)
        files: 3, // Max 3 files
    },
}).array('images', 3);

// Image compression settings
const IMAGE_CONFIG = {
    maxWidth: 1920,      // Max width in pixels
    maxHeight: 1920,     // Max height in pixels
    quality: 80,         // JPEG/WebP quality (0-100)
    pngCompressionLevel: 8, // PNG compression level (0-9)
};

// Process and compress a single image
const processImage = async (file) => {
    const uniqueSuffix = Date.now() + '-' + Math.round(Math.random() * 1E9);
    
    // Determine output format based on original mimetype
    let outputFormat = 'jpeg';
    let ext = '.jpg';
    
    if (file.mimetype === 'image/png') {
        outputFormat = 'png';
        ext = '.png';
    } else if (file.mimetype === 'image/webp') {
        outputFormat = 'webp';
        ext = '.webp';
    } else if (file.mimetype === 'image/gif') {
        // For GIF, convert to PNG to preserve transparency but remove animation
        outputFormat = 'png';
        ext = '.png';
    }
    
    const filename = `edit-request-${uniqueSuffix}${ext}`;
    const outputPath = path.join(uploadDir, filename);
    
    // Create sharp instance
    let sharpInstance = sharp(file.buffer);
    
    // Get image metadata
    const metadata = await sharpInstance.metadata();
    
    // Resize if needed (maintain aspect ratio, only downscale)
    if (metadata.width > IMAGE_CONFIG.maxWidth || metadata.height > IMAGE_CONFIG.maxHeight) {
        sharpInstance = sharpInstance.resize(IMAGE_CONFIG.maxWidth, IMAGE_CONFIG.maxHeight, {
            fit: 'inside',
            withoutEnlargement: true,
        });
    }
    
    // Apply format-specific compression
    if (outputFormat === 'jpeg') {
        sharpInstance = sharpInstance.jpeg({
            quality: IMAGE_CONFIG.quality,
            mozjpeg: true, // Use mozjpeg for better compression
        });
    } else if (outputFormat === 'png') {
        sharpInstance = sharpInstance.png({
            compressionLevel: IMAGE_CONFIG.pngCompressionLevel,
            adaptiveFiltering: true,
        });
    } else if (outputFormat === 'webp') {
        sharpInstance = sharpInstance.webp({
            quality: IMAGE_CONFIG.quality,
        });
    }
    
    // Save compressed image
    await sharpInstance.toFile(outputPath);
    
    // Get the final file stats
    const stats = fs.statSync(outputPath);
    
    return {
        filename,
        originalname: file.originalname,
        mimetype: `image/${outputFormat}`,
        size: stats.size,
        path: outputPath,
    };
};

// Middleware wrapper with error handling and image compression
const handleScoreEditImageUpload = (req, res, next) => {
    uploadScoreEditImages(req, res, async (err) => {
        if (err instanceof multer.MulterError) {
            if (err.code === 'LIMIT_FILE_SIZE') {
                return res.status(400).json({
                    success: false,
                    message: 'File size too large. Maximum 10MB per file.',
                });
            }
            if (err.code === 'LIMIT_FILE_COUNT') {
                return res.status(400).json({
                    success: false,
                    message: 'Too many files. Maximum 3 images allowed.',
                });
            }
            return res.status(400).json({
                success: false,
                message: err.message,
            });
        } else if (err) {
            return res.status(400).json({
                success: false,
                message: err.message,
            });
        }
        
        // Process and compress images if any were uploaded
        if (req.files && req.files.length > 0) {
            try {
                const processedFiles = await Promise.all(
                    req.files.map(file => processImage(file))
                );
                
                // Replace req.files with processed file info
                req.files = processedFiles;
                
                // Log compression stats
                console.log('[Image Upload] Processed images:');
                processedFiles.forEach((file, i) => {
                    const originalSize = req.files[i]?.buffer?.length || 'N/A';
                    console.log(`  - ${file.originalname}: ${file.size} bytes (compressed)`);
                });
            } catch (processError) {
                console.error('[Image Upload] Processing error:', processError);
                return res.status(500).json({
                    success: false,
                    message: 'Failed to process uploaded images.',
                });
            }
        }
        
        next();
    });
};

// Avatar upload configuration
const avatarUploadDir = path.join(__dirname, '../../uploads/avatars');
if (!fs.existsSync(avatarUploadDir)) {
    fs.mkdirSync(avatarUploadDir, { recursive: true });
}

// Avatar upload settings
const AVATAR_CONFIG = {
    maxSize: 256,        // Max size in pixels (square)
    quality: 85,         // JPEG quality
};

// Create multer instance for avatar
const uploadAvatar = multer({
    storage: memoryStorage,
    fileFilter,
    limits: {
        fileSize: 5 * 1024 * 1024, // 5MB max
        files: 1,
    },
}).single('avatar');

// Process and resize avatar to a square
const processAvatar = async (file, userId) => {
    const uniqueSuffix = Date.now() + '-' + Math.round(Math.random() * 1E9);
    const filename = `avatar-${userId}-${uniqueSuffix}.jpg`;
    const outputPath = path.join(avatarUploadDir, filename);
    
    // Create sharp instance and resize to square
    await sharp(file.buffer)
        .resize(AVATAR_CONFIG.maxSize, AVATAR_CONFIG.maxSize, {
            fit: 'cover',
            position: 'center',
        })
        .jpeg({
            quality: AVATAR_CONFIG.quality,
            mozjpeg: true,
        })
        .toFile(outputPath);
    
    return {
        filename,
        path: `/uploads/avatars/${filename}`,
    };
};

// Middleware wrapper for avatar upload
const handleAvatarUpload = (req, res, next) => {
    uploadAvatar(req, res, async (err) => {
        if (err instanceof multer.MulterError) {
            if (err.code === 'LIMIT_FILE_SIZE') {
                return res.status(400).json({
                    success: false,
                    message: 'ไฟล์มีขนาดใหญ่เกินไป สูงสุด 5MB',
                });
            }
            return res.status(400).json({
                success: false,
                message: err.message,
            });
        } else if (err) {
            return res.status(400).json({
                success: false,
                message: err.message,
            });
        }
        
        next();
    });
};

module.exports = {
    handleScoreEditImageUpload,
    handleAvatarUpload,
    processAvatar,
};

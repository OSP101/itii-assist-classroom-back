const { Resend } = require('resend');
const nodemailer = require('nodemailer');
const logger = require('./logger');
const config = require('../config');

// Get email provider at runtime (not module load time)
const getProvider = () => {
  return process.env.EMAIL_PROVIDER || (process.env.RESEND_API_KEY ? 'resend' : 'smtp');
};

// Initialize Resend client
let resend = null;
const getResendClient = () => {
  if (!resend && process.env.RESEND_API_KEY) {
    resend = new Resend(process.env.RESEND_API_KEY);
  }
  return resend;
};

// Create nodemailer transporter
const createTransporter = () => {
  if (!config.email.user || !config.email.pass) {
    logger.info('⚠️ Email service: No SMTP credentials configured. Emails will be logged to console.');
    return null;
  }

  return nodemailer.createTransport({
    host: config.email.host,
    port: config.email.port,
    secure: config.email.secure,
    auth: {
      user: config.email.user,
      pass: config.email.pass,
    },
  });
};

let transporter = null;
const getTransporter = () => {
  if (!transporter) {
    transporter = createTransporter();
  }
  return transporter;
};

/**
 * Generate 2FA verification code email HTML
 */
const get2FACodeHTML = (code, userName) => `
<!DOCTYPE html>
<html lang="th">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="X-UA-Compatible" content="IE=edge">
  <title>รหัสยืนยันตัวตน - ${config.twoFactor.appName}</title>
  <!--[if mso]>
  <noscript>
    <xml>
      <o:OfficeDocumentSettings>
        <o:PixelsPerInch>96</o:PixelsPerInch>
      </o:OfficeDocumentSettings>
    </xml>
  </noscript>
  <![endif]-->
</head>
<body style="margin: 0; padding: 0; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #eef2f7; -webkit-font-smoothing: antialiased; -moz-osx-font-smoothing: grayscale;">
  <!-- Preheader Text -->
  <div style="display: none; max-height: 0; overflow: hidden;">
    รหัสยืนยันของคุณคือ ${code} - ใช้ได้ภายใน 5 นาที
  </div>
  
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color: #eef2f7; padding: 48px 16px;">
    <tr>
      <td align="center">
        <!-- Main Container -->
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width: 480px; background-color: #ffffff; border-radius: 24px; box-shadow: 0 8px 32px rgba(0,0,0,0.08); overflow: hidden;">
          
          <!-- Header with Logo -->
          <tr>
            <td style="background: linear-gradient(145deg, #1e3a8a 0%, #1d4ed8 50%, #3b82f6 100%); padding: 40px 32px; text-align: center;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center">
                    <!-- Logo Circle -->
                    <div style="width: 72px; height: 72px; background: rgba(255,255,255,0.15); border-radius: 50%; display: inline-block; line-height: 72px; margin-bottom: 20px; border: 3px solid rgba(255,255,255,0.3);">
                      <span style="font-size: 36px;">🔐</span>
                    </div>
                  </td>
                </tr>
                <tr>
                  <td align="center">
                    <h1 style="color: #ffffff; margin: 0; font-size: 24px; font-weight: 700; letter-spacing: -0.5px;">
                      รหัสยืนยันตัวตน
                    </h1>
                    <p style="color: rgba(255,255,255,0.9); margin: 10px 0 0; font-size: 15px; font-weight: 400;">
                      ${config.twoFactor.appName}
                    </p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Content -->
          <tr>
            <td style="padding: 40px 32px 32px;">
              <!-- Greeting -->
              <p style="color: #1e293b; font-size: 17px; line-height: 1.7; margin: 0 0 20px; font-weight: 500;">
                สวัสดีครับ คุณ${userName || 'ผู้ใช้'},
              </p>
              <p style="color: #64748b; font-size: 15px; line-height: 1.7; margin: 0 0 32px;">
                มีการร้องขอรหัสยืนยันตัวตนสำหรับบัญชีของคุณ กรุณาใช้รหัสด้านล่างเพื่อดำเนินการต่อ
              </p>
              
              <!-- Code Box -->
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center">
                    <table role="presentation" cellpadding="0" cellspacing="0" style="background: linear-gradient(135deg, #f0f9ff 0%, #e0f2fe 100%); border: 2px solid #0ea5e9; border-radius: 20px; padding: 28px 40px;">
                      <tr>
                        <td align="center">
                          <p style="color: #0369a1; font-size: 11px; margin: 0 0 12px; text-transform: uppercase; letter-spacing: 3px; font-weight: 700;">
                            รหัสยืนยัน 6 หลัก
                          </p>
                          <p style="color: #0c4a6e; font-size: 42px; font-weight: 800; margin: 0; letter-spacing: 12px; font-family: 'SF Mono', 'Cascadia Code', Consolas, monospace;">
                            ${code}
                          </p>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>
              
              <!-- Timer Warning -->
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin-top: 28px;">
                <tr>
                  <td style="background: linear-gradient(135deg, #fef3c7 0%, #fde68a 100%); border-radius: 14px; padding: 18px 20px;">
                    <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                      <tr>
                        <td width="44" valign="top">
                          <div style="width: 36px; height: 36px; background: #fbbf24; border-radius: 10px; text-align: center; line-height: 36px;">
                            <span style="font-size: 18px;">⏱️</span>
                          </div>
                        </td>
                        <td style="padding-left: 14px;">
                          <p style="color: #92400e; font-size: 14px; margin: 0; line-height: 1.5; font-weight: 600;">
                            รหัสนี้จะหมดอายุใน 5 นาที
                          </p>
                          <p style="color: #a16207; font-size: 13px; margin: 4px 0 0; line-height: 1.4;">
                            สามารถใช้ได้เพียงครั้งเดียวเท่านั้น
                          </p>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Security Warning -->
          <tr>
            <td style="padding: 0 32px 32px;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background: linear-gradient(135deg, #fef2f2 0%, #fee2e2 100%); border-radius: 14px; border-left: 5px solid #ef4444; padding: 20px;">
                <tr>
                  <td>
                    <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                      <tr>
                        <td width="36" valign="top">
                          <span style="font-size: 22px;">🚫</span>
                        </td>
                        <td style="padding-left: 10px;">
                          <p style="color: #991b1b; font-size: 14px; margin: 0 0 8px; font-weight: 700;">
                            อย่าส่งรหัสนี้ให้ใครเด็ดขาด!
                          </p>
                          <p style="color: #b91c1c; font-size: 13px; margin: 0; line-height: 1.6;">
                            ทีมงาน ${config.twoFactor.appName} จะไม่มีวันขอรหัสนี้จากคุณ หากมีใครขอรหัสนี้ แสดงว่าพวกเขากำลังพยายามเข้าถึงบัญชีของคุณ
                          </p>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Didn't Request Section -->
          <tr>
            <td style="padding: 0 32px 40px;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background: #f8fafc; border-radius: 14px; padding: 20px; border: 1px solid #e2e8f0;">
                <tr>
                  <td>
                    <p style="color: #475569; font-size: 14px; margin: 0 0 12px; font-weight: 600;">
                      🤔 ไม่ได้เป็นคนร้องขอรหัสนี้?
                    </p>
                    <p style="color: #64748b; font-size: 13px; margin: 0; line-height: 1.7;">
                      หากคุณไม่ได้ร้องขอรหัสนี้ <strong style="color: #dc2626;">แสดงว่ามีคนอื่นรู้รหัสผ่านของคุณแล้ว!</strong> กรุณาเปลี่ยนรหัสผ่านของคุณทันทีเพื่อความปลอดภัยของบัญชี
                    </p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Divider -->
          <tr>
            <td style="padding: 0 32px;">
              <div style="height: 1px; background: linear-gradient(to right, transparent, #e2e8f0, transparent);"></div>
            </td>
          </tr>
          
          <!-- Footer -->
          <tr>
            <td style="padding: 28px 32px; text-align: center;">
              <p style="color: #94a3b8; font-size: 12px; margin: 0 0 8px; line-height: 1.6;">
                อีเมลนี้ถูกส่งโดยอัตโนมัติจากระบบ ${config.twoFactor.appName}<br>
                กรุณาอย่าตอบกลับอีเมลนี้
              </p>
              <p style="color: #cbd5e1; font-size: 11px; margin: 0;">
                © ${new Date().getFullYear()} ${config.twoFactor.appName} - All rights reserved
              </p>
            </td>
          </tr>
          
        </table>
        
        <!-- Bottom Security Note -->
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width: 480px; margin-top: 24px;">
          <tr>
            <td align="center">
              <p style="color: #94a3b8; font-size: 11px; margin: 0; line-height: 1.6;">
                🔒 อีเมลนี้ถูกส่งจาก ${config.email.from}<br>
                ตรวจสอบให้แน่ใจว่าลิงก์ที่คุณเข้าถึงมาจาก ${config.twoFactor.appName} จริง
              </p>
            </td>
          </tr>
        </table>
        
      </td>
    </tr>
  </table>
</body>
</html>
`;

/**
 * Generate 2FA setup confirmation email HTML
 */
const get2FASetupHTML = (methodText, userName) => `
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="X-UA-Compatible" content="IE=edge">
  <title>เปิดใช้งาน 2FA สำเร็จ - ${config.twoFactor.appName}</title>
</head>
<body style="margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif; background-color: #f4f7fa; -webkit-font-smoothing: antialiased;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color: #f4f7fa; padding: 40px 20px;">
    <tr>
      <td align="center">
        <!-- Main Container -->
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width: 520px; background-color: #ffffff; border-radius: 16px; box-shadow: 0 4px 24px rgba(0,0,0,0.08); overflow: hidden;">
          
          <!-- Header -->
          <tr>
            <td style="background: linear-gradient(135deg, #059669 0%, #047857 100%); padding: 32px 24px; text-align: center;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center">
                    <!-- Success Icon -->
                    <div style="width: 56px; height: 56px; background: rgba(255,255,255,0.2); border-radius: 16px; display: inline-block; line-height: 56px; margin-bottom: 16px;">
                      <span style="font-size: 28px;">✅</span>
                    </div>
                  </td>
                </tr>
                <tr>
                  <td align="center">
                    <h1 style="color: #ffffff; margin: 0; font-size: 22px; font-weight: 600; letter-spacing: -0.3px;">
                      เปิดใช้งาน 2FA สำเร็จ
                    </h1>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Content -->
          <tr>
            <td style="padding: 32px 24px;">
              <p style="color: #1f2937; font-size: 16px; line-height: 1.6; margin: 0 0 24px;">
                สวัสดีคุณ <strong>${userName || 'ผู้ใช้'}</strong>,
              </p>
              <p style="color: #6b7280; font-size: 15px; line-height: 1.6; margin: 0 0 24px;">
                การยืนยันตัวตนสองขั้นตอน (2FA) ของบัญชีคุณได้ถูกเปิดใช้งานแล้วโดยใช้ <strong>${methodText}</strong>
              </p>
              
              <!-- Success Box -->
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td style="background-color: #ecfdf5; border-left: 4px solid #10b981; padding: 16px; border-radius: 0 12px 12px 0;">
                    <table role="presentation" cellpadding="0" cellspacing="0">
                      <tr>
                        <td style="padding-right: 10px; vertical-align: top;">
                          <span style="font-size: 18px;">🛡️</span>
                        </td>
                        <td>
                          <p style="color: #065f46; font-size: 14px; margin: 0; line-height: 1.4;">
                            <strong>บัญชีของคุณมีความปลอดภัยมากขึ้นแล้ว!</strong><br>
                            การเข้าสู่ระบบครั้งต่อไปจะต้องใช้รหัสยืนยันเพิ่มเติม
                          </p>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>
              
              <!-- Important Notes -->
              <div style="margin-top: 24px;">
                <p style="color: #4b5563; font-size: 14px; margin: 0 0 12px; font-weight: 600;">
                  📌 สิ่งที่ควรทำ:
                </p>
                <ul style="color: #6b7280; font-size: 14px; margin: 0; padding-left: 20px; line-height: 1.8;">
                  <li>เก็บรหัสสำรอง (Recovery Codes) ไว้ในที่ปลอดภัย</li>
                  <li>อย่าเปิดเผยรหัสยืนยันให้ผู้อื่น</li>
                  <li>หากสงสัยว่าบัญชีถูกเข้าถึง กรุณาเปลี่ยนรหัสผ่านทันที</li>
                </ul>
              </div>
            </td>
          </tr>
          
          <!-- Footer -->
          <tr>
            <td style="background-color: #f9fafb; padding: 20px 24px; border-top: 1px solid #e5e7eb;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center">
                    <p style="color: #9ca3af; font-size: 12px; margin: 0; line-height: 1.5;">
                      © ${new Date().getFullYear()} ${config.twoFactor.appName}<br>
                      อีเมลนี้ส่งโดยอัตโนมัติ กรุณาอย่าตอบกลับ
                    </p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
        </table>
      </td>
    </tr>
  </table>
</body>
</html>
`;

/**
 * Send email using Resend API
 */
const sendWithResend = async (options) => {
  const resendClient = getResendClient();
  if (!resendClient) {
    throw new Error('Resend API key not configured');
  }

  const { data, error } = await resendClient.emails.send({
    from: options.from || `${config.twoFactor.appName} <${config.email.from}>`,
    to: options.to,
    subject: options.subject,
    html: options.html,
    text: options.text,
  });

  if (error) {
    throw new Error(error.message);
  }

  return { success: true, messageId: data?.id };
};

/**
 * Send email using SMTP
 */
const sendWithSMTP = async (options) => {
  const emailTransporter = getTransporter();
  if (!emailTransporter) {
    // Dev mode - log to console
    logger.info('📧 [DEV] Email would be sent:');
    logger.info('   To:', options.to);
    logger.info('   Subject:', options.subject);
    if (options.code) logger.info('   Code:', options.code);
    return { success: true, messageId: 'dev-mode' };
  }

  const info = await emailTransporter.sendMail({
    from: options.from || `"${config.twoFactor.appName}" <${config.email.from}>`,
    to: options.to,
    subject: options.subject,
    html: options.html,
    text: options.text,
  });

  return { success: true, messageId: info.messageId };
};

/**
 * Send email (auto-select provider)
 */
const sendEmail = async (options) => {
  try {
    const provider = getProvider();
    if (provider === 'resend' && process.env.RESEND_API_KEY) {
      return await sendWithResend(options);
    }
    return await sendWithSMTP(options);
  } catch (error) {
    logger.error('📧 Failed to send email:', error);
    throw error;
  }
};

/**
 * Send 2FA verification code via email
 */
const send2FACode = async (to, code, userName) => {
  const html = get2FACodeHTML(code, userName);
  const subject = `รหัสยืนยันตัวตน - ${config.twoFactor.appName}`;
  const text = `รหัสยืนยันตัวตนของคุณคือ: ${code}\n\nรหัสนี้จะหมดอายุภายใน 5 นาทีและใช้ได้เพียงครั้งเดียว\n\nหากคุณไม่ได้ร้องขอรหัสนี้ กรุณาเพิกเฉยอีเมลนี้`;

  try {
    const result = await sendEmail({ to, subject, html, text, code });
    logger.info('📧 2FA email sent to:', to, 'MessageID:', result.messageId);
    return result;
  } catch (error) {
    logger.error('📧 Failed to send 2FA email:', error);
    throw error;
  }
};

/**
 * Send 2FA setup confirmation email
 */
const send2FASetupEmail = async (to, method, userName) => {
  const methodText = method === 'totp' ? 'แอป Authenticator' : 'อีเมล';
  const html = get2FASetupHTML(methodText, userName);
  const subject = `เปิดใช้งานการยืนยันตัวตนสองขั้นตอนสำเร็จ - ${config.twoFactor.appName}`;
  const text = `การยืนยันตัวตนสองขั้นตอน (2FA) ได้ถูกเปิดใช้งานแล้วโดยใช้ ${methodText}`;

  try {
    const result = await sendEmail({ to, subject, html, text });
    logger.info('📧 2FA setup email sent to:', to);
    return result;
  } catch (error) {
    logger.error('📧 Failed to send 2FA setup email:', error);
    // Don't throw - this is not critical
    return { success: false, error };
  }
};

/**
 * Generate password reset email HTML
 */
const getPasswordResetHTML = (resetUrl, userName) => `
<!DOCTYPE html>
<html lang="th">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="X-UA-Compatible" content="IE=edge">
  <title>รีเซ็ตรหัสผ่าน - ${config.twoFactor.appName}</title>
</head>
<body style="margin: 0; padding: 0; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #eef2f7; -webkit-font-smoothing: antialiased;">
  <div style="display: none; max-height: 0; overflow: hidden;">
    คุณได้ร้องขอรีเซ็ตรหัสผ่าน - ลิงก์นี้มีอายุ 1 ชั่วโมง
  </div>
  
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color: #eef2f7; padding: 48px 16px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width: 480px; background-color: #ffffff; border-radius: 24px; box-shadow: 0 8px 32px rgba(0,0,0,0.08); overflow: hidden;">
          
          <!-- Header -->
          <tr>
            <td style="background: linear-gradient(145deg, #dc2626 0%, #ef4444 50%, #f87171 100%); padding: 40px 32px; text-align: center;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center" style="padding-bottom: 20px;">
                    <div style="width: 72px; height: 72px; background: rgba(255,255,255,0.2); border-radius: 20px; display: inline-block; line-height: 72px; text-align: center;">
                      <span style="font-size: 36px;">🔑</span>
                    </div>
                  </td>
                </tr>
                <tr>
                  <td align="center">
                    <h1 style="color: #ffffff; margin: 0 0 8px; font-size: 26px; font-weight: 700; letter-spacing: -0.5px;">
                      รีเซ็ตรหัสผ่าน
                    </h1>
                    <p style="color: rgba(255,255,255,0.9); margin: 0; font-size: 15px;">
                      ${config.twoFactor.appName}
                    </p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Content -->
          <tr>
            <td style="padding: 36px 32px;">
              <p style="color: #1e293b; font-size: 17px; margin: 0 0 8px; font-weight: 600;">
                สวัสดี${userName ? ` ${userName}` : ''}! 👋
              </p>
              <p style="color: #475569; font-size: 15px; margin: 0 0 28px; line-height: 1.7;">
                เราได้รับคำขอรีเซ็ตรหัสผ่านสำหรับบัญชีของคุณ กดปุ่มด้านล่างเพื่อตั้งรหัสผ่านใหม่
              </p>
              
              <!-- Reset Button -->
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center">
                    <a href="${resetUrl}" style="display: inline-block; background: linear-gradient(135deg, #dc2626 0%, #ef4444 100%); color: #ffffff; text-decoration: none; padding: 16px 48px; border-radius: 14px; font-size: 16px; font-weight: 600; box-shadow: 0 4px 16px rgba(239, 68, 68, 0.4);">
                      🔒 ตั้งรหัสผ่านใหม่
                    </a>
                  </td>
                </tr>
              </table>

              <!-- Link fallback -->
              <p style="color: #64748b; font-size: 12px; margin: 24px 0 0; text-align: center; line-height: 1.6;">
                หากปุ่มด้านบนไม่ทำงาน คัดลอกลิงก์นี้ไปวางในเบราว์เซอร์:<br>
                <a href="${resetUrl}" style="color: #3b82f6; word-break: break-all;">${resetUrl}</a>
              </p>
              
              <!-- Timer Warning -->
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="margin-top: 28px;">
                <tr>
                  <td style="background: linear-gradient(135deg, #fef3c7 0%, #fde68a 100%); border-radius: 14px; padding: 18px 20px;">
                    <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
                      <tr>
                        <td width="44" valign="top">
                          <div style="width: 36px; height: 36px; background: #fbbf24; border-radius: 10px; text-align: center; line-height: 36px;">
                            <span style="font-size: 18px;">⏱️</span>
                          </div>
                        </td>
                        <td style="padding-left: 14px;">
                          <p style="color: #92400e; font-size: 14px; margin: 0; line-height: 1.5; font-weight: 600;">
                            ลิงก์นี้จะหมดอายุใน 1 ชั่วโมง
                          </p>
                          <p style="color: #a16207; font-size: 13px; margin: 4px 0 0; line-height: 1.4;">
                            สามารถใช้ได้เพียงครั้งเดียวเท่านั้น
                          </p>
                        </td>
                      </tr>
                    </table>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Didn't Request Section -->
          <tr>
            <td style="padding: 0 32px 40px;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background: #f8fafc; border-radius: 14px; padding: 20px; border: 1px solid #e2e8f0;">
                <tr>
                  <td>
                    <p style="color: #475569; font-size: 14px; margin: 0 0 12px; font-weight: 600;">
                      🤔 ไม่ได้เป็นคนร้องขอ?
                    </p>
                    <p style="color: #64748b; font-size: 13px; margin: 0; line-height: 1.7;">
                      หากคุณไม่ได้ร้องขอรีเซ็ตรหัสผ่าน คุณสามารถเพิกเฉยอีเมลนี้ได้ ลิงก์จะหมดอายุโดยอัตโนมัติ
                    </p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          
          <!-- Footer -->
          <tr>
            <td style="background: linear-gradient(135deg, #f1f5f9 0%, #e2e8f0 100%); padding: 28px; text-align: center; border-top: 1px solid #e2e8f0;">
              <p style="color: #64748b; font-size: 12px; margin: 0 0 4px;">
                © 2026 ${config.twoFactor.appName}
              </p>
              <p style="color: #94a3b8; font-size: 11px; margin: 0;">
                อีเมลนี้ถูกส่งโดยอัตโนมัติ กรุณาอย่าตอบกลับ
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>
`;

/**
 * Send password reset email
 */
const sendPasswordResetEmail = async (to, resetUrl, userName) => {
  const html = getPasswordResetHTML(resetUrl, userName);
  const subject = `รีเซ็ตรหัสผ่าน - ${config.twoFactor.appName}`;
  const text = `คุณได้ร้องขอรีเซ็ตรหัสผ่าน\n\nกดลิงก์นี้เพื่อตั้งรหัสผ่านใหม่: ${resetUrl}\n\nลิงก์นี้จะหมดอายุใน 1 ชั่วโมง\n\nหากคุณไม่ได้ร้องขอ กรุณาเพิกเฉยอีเมลนี้`;

  try {
    const result = await sendEmail({ to, subject, html, text });
    logger.info('📧 Password reset email sent to:', to, 'MessageID:', result.messageId);
    return result;
  } catch (error) {
    logger.error('📧 Failed to send password reset email:', error);
    throw error;
  }
};

module.exports = {
  send2FACode,
  send2FASetupEmail,
  sendPasswordResetEmail,
  sendEmail,
};

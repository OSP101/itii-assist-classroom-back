package services

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// วินิจฉัยการเชื่อมต่อ SMTP ทีละขั้น (หน้าตั้งค่าของ admin)
// =============================================================================

type EmailDiagnosticStep struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // ok, fail, skip, warn
	Detail    string `json:"detail"`
	Hint      string `json:"hint,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

type EmailDiagnosticReport struct {
	Provider string                `json:"provider"`
	Target   string                `json:"target"`
	Mode     string                `json:"mode"` // implicit_tls, starttls, plaintext, resend
	Steps    []EmailDiagnosticStep `json:"steps"`
	Overall  string                `json:"overall"` // ok, fail
	Advice   []string              `json:"advice"`
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	}
	return fmt.Sprintf("0x%04x", v)
}

func describeTLSState(state tls.ConnectionState) string {
	parts := []string{tlsVersionName(state.Version), tls.CipherSuiteName(state.CipherSuite)}
	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		parts = append(parts, fmt.Sprintf("cert CN=%s issuer=%s expires=%s", cert.Subject.CommonName, cert.Issuer.CommonName, cert.NotAfter.Format("2006-01-02")))
		if len(cert.DNSNames) > 0 {
			parts = append(parts, "SAN="+strings.Join(cert.DNSNames, ","))
		}
	}
	return strings.Join(parts, " | ")
}

// DiagnoseEmailTransport ไล่เช็ก DNS → TCP → greeting → EHLO → TLS → AUTH โดยไม่ส่งเมลจริง
func DiagnoseEmailTransport() EmailDiagnosticReport {
	cfg := loadEmailConfig()
	report := EmailDiagnosticReport{Provider: cfg.Provider, Advice: []string{}}
	addStep := func(name, status, detail, hint string, started time.Time) {
		report.Steps = append(report.Steps, EmailDiagnosticStep{Name: name, Status: status, Detail: detail, Hint: hint, ElapsedMS: time.Since(started).Milliseconds()})
	}

	if cfg.Provider == "resend" {
		report.Mode = "resend"
		report.Target = "https://api.resend.com"
		started := time.Now()
		if cfg.ResendKey == "" {
			addStep("api_key", "fail", "RESEND_API_KEY ว่าง", "ตั้งค่า RESEND_API_KEY ใน env ของ backend", started)
			report.Overall = "fail"
			return report
		}
		addStep("api_key", "ok", "RESEND_API_KEY ตั้งค่าแล้ว", "", started)
		started = time.Now()
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", "api.resend.com:443", &tls.Config{ServerName: "api.resend.com"})
		if err != nil {
			addStep("https", "fail", err.Error(), "เครื่องนี้ออกอินเทอร์เน็ตไปยัง api.resend.com:443 ไม่ได้", started)
			report.Overall = "fail"
			return report
		}
		addStep("https", "ok", describeTLSState(conn.ConnectionState()), "", started)
		conn.Close()
		report.Overall = "ok"
		return report
	}

	addr := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	report.Target = addr
	switch {
	case cfg.SMTPSecure && cfg.SMTPPort == 465:
		report.Mode = "implicit_tls"
	case cfg.SMTPSecure:
		report.Mode = "starttls"
	default:
		report.Mode = "plaintext"
	}

	started := time.Now()
	if cfg.SMTPHost == "" {
		addStep("config", "fail", "SMTP_HOST ว่าง", "ตั้งค่า SMTP_HOST, SMTP_PORT, SMTP_SECURE", started)
		report.Overall = "fail"
		return report
	}
	addStep("config", "ok", fmt.Sprintf("host=%s port=%d secure=%t auth=%t skip_verify=%t legacy_ciphers=%t min_tls=%s", cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPSecure, cfg.SMTPUser != "", cfg.SMTPTLSSkipVerify, cfg.SMTPTLSLegacyCiphers, func() string {
		if cfg.SMTPTLSMinVersion == 0 {
			return "default(1.2)"
		}
		return tlsVersionName(cfg.SMTPTLSMinVersion)
	}()), "", started)

	started = time.Now()
	ips, err := net.LookupHost(cfg.SMTPHost)
	if err != nil {
		addStep("dns", "fail", err.Error(), "ชื่อโฮสต์ resolve ไม่ได้จากเครื่อง backend", started)
		report.Overall = "fail"
		return report
	}
	addStep("dns", "ok", strings.Join(ips, ", "), "", started)

	started = time.Now()
	rawConn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		addStep("tcp", "fail", err.Error(), "พอร์ตถูกบล็อกหรือเซิร์ฟเวอร์ไม่เปิดพอร์ตนี้ (ผู้ให้บริการ VPS หลายเจ้าบล็อกพอร์ต 25)", started)
		report.Overall = "fail"
		return report
	}
	addStep("tcp", "ok", "connected to "+rawConn.RemoteAddr().String(), "", started)

	var client *smtp.Client
	if report.Mode == "implicit_tls" {
		started = time.Now()
		tlsConn := tls.Client(rawConn, smtpTLSConfig(cfg))
		tlsConn.SetDeadline(time.Now().Add(10 * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			rawConn.Close()
			addStep("tls_implicit", "fail", err.Error(), smtpTLSHint(err), started)
			report.Overall = "fail"
			report.Advice = append(report.Advice, "ลองสลับโหมด: หากเซิร์ฟเวอร์ใช้ STARTTLS ให้ตั้ง SMTP_PORT=587 (SMTP_SECURE=true)")
			return report
		}
		tlsConn.SetDeadline(time.Time{})
		addStep("tls_implicit", "ok", describeTLSState(tlsConn.ConnectionState()), "", started)
		started = time.Now()
		client, err = smtp.NewClient(tlsConn, cfg.SMTPHost)
		if err != nil {
			tlsConn.Close()
			addStep("greeting", "fail", err.Error(), "เซิร์ฟเวอร์ไม่ตอบ 220 หลัง TLS", started)
			report.Overall = "fail"
			return report
		}
		addStep("greeting", "ok", "220 received", "", started)
	} else {
		started = time.Now()
		rawConn.SetDeadline(time.Now().Add(10 * time.Second))
		client, err = smtp.NewClient(rawConn, cfg.SMTPHost)
		if err != nil {
			rawConn.Close()
			addStep("greeting", "fail", err.Error(), "เซิร์ฟเวอร์ไม่ตอบ 220 แบบ plaintext: หากพอร์ตนี้เป็น implicit TLS (465) ให้ตั้ง SMTP_PORT=465 และ SMTP_SECURE=true", started)
			report.Overall = "fail"
			return report
		}
		rawConn.SetDeadline(time.Time{})
		addStep("greeting", "ok", "220 received", "", started)
	}
	defer client.Close()

	started = time.Now()
	if err := client.Hello("localhost"); err != nil {
		addStep("ehlo", "fail", err.Error(), "", started)
		report.Overall = "fail"
		return report
	}
	exts := []string{}
	for _, name := range []string{"STARTTLS", "AUTH", "8BITMIME", "SIZE", "PIPELINING", "SMTPUTF8"} {
		if ok, param := client.Extension(name); ok {
			if param != "" {
				exts = append(exts, name+"="+param)
			} else {
				exts = append(exts, name)
			}
		}
	}
	addStep("ehlo", "ok", "extensions: "+strings.Join(exts, ", "), "", started)

	if report.Mode == "starttls" {
		started = time.Now()
		if ok, _ := client.Extension("STARTTLS"); !ok {
			addStep("starttls", "fail", "server does not advertise STARTTLS", "ตั้ง SMTP_SECURE=false หากเป็น relay ภายในแบบไม่เข้ารหัส หรือใช้พอร์ต 465 หากเป็น implicit TLS", started)
			report.Overall = "fail"
			return report
		}
		if err := client.StartTLS(smtpTLSConfig(cfg)); err != nil {
			addStep("starttls", "fail", err.Error(), smtpTLSHint(err), started)
			report.Overall = "fail"
			if isTLSHandshakeFailure(err) && !cfg.SMTPTLSLegacyCiphers {
				// ลองซ้ำด้วย cipher ชุดเต็มบน connection ใหม่ เพื่อบอกได้ชัดว่าแก้ด้วย env ตัวไหน
				retryStarted := time.Now()
				legacyCfg := cfg
				legacyCfg.SMTPTLSLegacyCiphers = true
				if state, retryErr := probeStartTLS(addr, legacyCfg); retryErr == nil {
					addStep("starttls_legacy_retry", "ok", describeTLSState(state), "ตั้ง SMTP_TLS_LEGACY_CIPHERS=true แล้วส่งได้ (ระบบจะลองซ้ำให้อัตโนมัติอยู่แล้ว แต่ตั้งไว้จะเร็วกว่า)", retryStarted)
					report.Advice = append(report.Advice, "ตั้ง SMTP_TLS_LEGACY_CIPHERS=true ใน env ของ backend (relay รับเฉพาะ cipher แบบ RSA key exchange)")
					report.Overall = "ok"
					return report
				} else {
					addStep("starttls_legacy_retry", "fail", retryErr.Error(), "", retryStarted)
				}
			}
			if isTLSHandshakeFailure(err) {
				report.Advice = append(report.Advice,
					"ลอง SMTP_TLS_MIN_VERSION=1.0 (relay เก่า)",
					"หากเป็น relay ภายในที่ใช้ self-signed ลอง SMTP_TLS_SKIP_VERIFY=true",
					"หาก relay ต้องการ client certificate ให้ติดต่อผู้ดูแลเมลเซิร์ฟเวอร์")
			}
			return report
		}
		if state, ok := client.TLSConnectionState(); ok {
			addStep("starttls", "ok", describeTLSState(state), "", started)
		} else {
			addStep("starttls", "ok", "negotiated", "", started)
		}
	} else if report.Mode == "plaintext" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			addStep("starttls", "warn", "server offers STARTTLS but SMTP_SECURE=false", "แนะนำตั้ง SMTP_SECURE=true", time.Now())
		} else {
			addStep("starttls", "skip", "plaintext relay", "", time.Now())
		}
	}

	started = time.Now()
	if cfg.SMTPUser == "" {
		addStep("auth", "skip", "no SMTP_USER, relying on IP allowlist", "", started)
	} else {
		if ok, mechs := client.Extension("AUTH"); !ok {
			addStep("auth", "fail", "server does not advertise AUTH", "หาก relay ใช้ IP allowlist ให้ลบ SMTP_USER/SMTP_PASS ออก", started)
			report.Overall = "fail"
			return report
		} else if err := client.Auth(smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)); err != nil {
			addStep("auth", "fail", err.Error()+" (mechanisms: "+mechs+")", "ตรวจ SMTP_USER/SMTP_PASS (บริการเช่น Gmail/Office365 ต้องใช้ app password)", started)
			report.Overall = "fail"
			return report
		}
		addStep("auth", "ok", "PLAIN auth accepted for "+cfg.SMTPUser, "", started)
	}

	started = time.Now()
	from := extractEmailAddress(cfg.From)
	if err := client.Mail(from); err != nil {
		addStep("mail_from", "fail", err.Error(), "เซิร์ฟเวอร์ปฏิเสธผู้ส่ง "+from+" ตรวจ EMAIL_FROM ให้เป็นโดเมนที่ relay อนุญาต", started)
		report.Overall = "fail"
		_ = client.Reset()
		return report
	}
	addStep("mail_from", "ok", "sender accepted: "+from, "", started)
	_ = client.Reset()
	_ = client.Quit()
	report.Overall = "ok"
	return report
}

// probeStartTLS เปิด connection ใหม่แล้วลอง STARTTLS ด้วย config ที่ให้ ใช้ในการวินิจฉัยเท่านั้น
func probeStartTLS(addr string, cfg emailConfig) (tls.ConnectionState, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return tls.ConnectionState{}, err
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return tls.ConnectionState{}, err
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return tls.ConnectionState{}, err
	}
	if err := client.StartTLS(smtpTLSConfig(cfg)); err != nil {
		return tls.ConnectionState{}, err
	}
	state, _ := client.TLSConnectionState()
	_ = client.Quit()
	return state, nil
}

package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const (
	EncryptionNone     = "none"
	EncryptionSSL      = "ssl"
	EncryptionStartTLS = "starttls"
)

type SMTPConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string

	From    Address
	Timeout time.Duration
}

type SMTPSender struct{ cfg SMTPConfig }

func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("SMTP 主机未配置")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("SMTP 端口不合法")
	}
	if strings.TrimSpace(cfg.From.Email) == "" {
		return nil, fmt.Errorf("发件人邮箱未配置")
	}
	switch cfg.Encryption {
	case "", EncryptionNone, EncryptionSSL, EncryptionStartTLS:
	default:
		return nil, fmt.Errorf("不支持的加密方式: %s", cfg.Encryption)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	return &SMTPSender{cfg: cfg}, nil
}

func (s *SMTPSender) Name() string { return "smtp" }

func (s *SMTPSender) Send(ctx context.Context, msg *Message) (res *Result, err error) {
	if msg == nil {
		return nil, &Error{Kind: KindConfiguration, Provider: "smtp", Message: "邮件内容为空"}
	}
	recipients := msg.Recipients()
	if len(recipients) == 0 {
		return nil, &Error{Kind: KindConfiguration, Provider: "smtp", Message: "没有收件人"}
	}
	from := msg.From
	if strings.TrimSpace(from.Email) == "" {
		from = s.cfg.From
	}
	body, berr := buildMIME(msg, from)
	if berr != nil {
		return nil, &Error{Kind: KindConfiguration, Provider: "smtp", Message: "构造邮件内容失败", Err: berr}
	}

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	dialer := &net.Dialer{Timeout: s.cfg.Timeout}
	var conn net.Conn
	var cerr error
	if s.cfg.Encryption == EncryptionSSL {
		conn, cerr = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.cfg.Host})
	} else {
		conn, cerr = dialer.DialContext(ctx, "tcp", addr)
	}
	if cerr != nil {
		return nil, &Error{Kind: KindTemporary, Provider: "smtp", Message: "连接 SMTP 服务器失败", Err: cerr}
	}
	defer func() { _ = conn.Close() }()

	client, xerr := smtp.NewClient(conn, s.cfg.Host)
	if xerr != nil {
		return nil, &Error{Kind: KindTemporary, Provider: "smtp", Message: "SMTP 握手失败", Err: xerr}
	}
	defer func() { _ = client.Close() }()

	if s.cfg.Encryption == EncryptionStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return nil, &Error{Kind: KindConfiguration, Provider: "smtp", Message: "服务器不支持 STARTTLS"}
		}
		if terr := client.StartTLS(&tls.Config{ServerName: s.cfg.Host}); terr != nil {
			return nil, &Error{Kind: KindConfiguration, Provider: "smtp", Message: "STARTTLS 升级失败", Err: terr}
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if aerr := client.Auth(auth); aerr != nil {

			return nil, &Error{Kind: KindConfiguration, Provider: "smtp", Message: "SMTP 认证失败", Err: aerr}
		}
	}
	if merr := client.Mail(from.Email); merr != nil {
		return nil, AsError("smtp", merr)
	}
	for _, rcpt := range recipients {
		if rerr := client.Rcpt(rcpt); rerr != nil {

			return nil, AsError("smtp", rerr)
		}
	}
	w, werr := client.Data()
	if werr != nil {
		return nil, AsError("smtp", werr)
	}
	if _, werr = w.Write(body); werr != nil {
		return nil, AsError("smtp", werr)
	}
	if cerr = w.Close(); cerr != nil {
		return nil, AsError("smtp", cerr)
	}
	_ = client.Quit()
	return &Result{Provider: "smtp", Accepted: recipients}, nil
}

func buildMIME(msg *Message, from Address) ([]byte, error) {
	var buf bytes.Buffer
	hdr := textproto.MIMEHeader{}
	hdr.Set("From", from.String())
	hdr.Set("To", joinAddresses(msg.To))
	if len(msg.Cc) > 0 {
		hdr.Set("Cc", joinAddresses(msg.Cc))
	}
	if msg.ReplyTo != nil && strings.TrimSpace(msg.ReplyTo.Email) != "" {
		hdr.Set("Reply-To", msg.ReplyTo.String())
	}
	hdr.Set("Subject", encodeHeader(msg.Subject))
	hdr.Set("Date", time.Now().Format(time.RFC1123Z))
	hdr.Set("MIME-Version", "1.0")
	for k, v := range msg.Headers {
		hdr.Set(k, v)
	}

	hasAttach := len(msg.Attachments) > 0
	hasAlt := strings.TrimSpace(msg.HTML) != "" && strings.TrimSpace(msg.Text) != ""

	switch {
	case hasAttach:
		mw := multipart.NewWriter(&buf)
		hdr.Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
		if _, err := buf.WriteString(renderHeader(hdr)); err != nil {
			return nil, err
		}

		if err := writeHTMLPart(mw, firstNonEmpty(msg.HTML, msg.Text)); err != nil {
			return nil, err
		}
		for _, att := range msg.Attachments {
			if err := writeAttachment(mw, att); err != nil {
				return nil, err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}
	case hasAlt:
		mw := multipart.NewWriter(&buf)
		hdr.Set("Content-Type", "multipart/alternative; boundary="+mw.Boundary())
		if _, err := buf.WriteString(renderHeader(hdr)); err != nil {
			return nil, err
		}
		if err := writeTextPart(mw, msg.Text); err != nil {
			return nil, err
		}
		if err := writeHTMLPart(mw, msg.HTML); err != nil {
			return nil, err
		}
		if err := mw.Close(); err != nil {
			return nil, err
		}
	default:
		ct := "text/plain; charset=UTF-8"
		body := msg.Text
		if strings.TrimSpace(msg.HTML) != "" {
			ct = "text/html; charset=UTF-8"
			body = msg.HTML
		}
		hdr.Set("Content-Type", ct)
		hdr.Set("Content-Transfer-Encoding", "quoted-printable")
		if _, err := buf.WriteString(renderHeader(hdr)); err != nil {
			return nil, err
		}
		if err := writeQuotedPrintable(&buf, body); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func renderHeader(h textproto.MIMEHeader) string {
	var b strings.Builder

	for _, key := range []string{"From", "To", "Cc", "Reply-To", "Subject", "Date", "MIME-Version", "Content-Type", "Content-Transfer-Encoding"} {
		if v := h.Get(key); v != "" {
			b.WriteString(key + ": " + v + "\r\n")
		}
	}
	for k, vs := range h {
		if isCanonicalHeader(k) {
			continue
		}
		for _, v := range vs {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("\r\n")
	return b.String()
}

func isCanonicalHeader(k string) bool {
	switch textproto.CanonicalMIMEHeaderKey(k) {
	case "From", "To", "Cc", "Reply-To", "Subject", "Date", "Mime-Version", "Content-Type", "Content-Transfer-Encoding":
		return true
	}
	return false
}

func writeTextPart(mw *multipart.Writer, text string) error {
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Type", "text/plain; charset=UTF-8")
	hdr.Set("Content-Transfer-Encoding", "quoted-printable")
	p, err := mw.CreatePart(hdr)
	if err != nil {
		return err
	}
	return writeQuotedPrintable(p, text)
}

func writeHTMLPart(mw *multipart.Writer, html string) error {
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Type", "text/html; charset=UTF-8")
	hdr.Set("Content-Transfer-Encoding", "quoted-printable")
	p, err := mw.CreatePart(hdr)
	if err != nil {
		return err
	}
	return writeQuotedPrintable(p, html)
}

func writeAttachment(mw *multipart.Writer, att Attachment) error {
	ct := att.ContentType
	if strings.TrimSpace(ct) == "" {
		ct = "application/octet-stream"
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Type", ct+"; name=\""+att.Filename+"\"")
	hdr.Set("Content-Transfer-Encoding", "base64")
	hdr.Set("Content-Disposition", "attachment; filename=\""+att.Filename+"\"")
	p, err := mw.CreatePart(hdr)
	if err != nil {
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, &lineBreaker{w: p, width: 76})
	if _, err := enc.Write(att.Data); err != nil {
		return err
	}
	return enc.Close()
}

type lineBreaker struct {
	w     interface{ Write([]byte) (int, error) }
	width int
	n     int
}

func (l *lineBreaker) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		space := l.width - l.n
		if space <= 0 {
			if _, err := l.w.Write([]byte("\r\n")); err != nil {
				return total, err
			}
			l.n = 0
			space = l.width
		}
		chunk := p
		if len(chunk) > space {
			chunk = chunk[:space]
		}
		n, err := l.w.Write(chunk)
		total += n
		l.n += n
		p = p[n:]
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func writeQuotedPrintable(w interface{ Write([]byte) (int, error) }, body string) error {
	qw := quotedprintable.NewWriter(w)
	if _, err := qw.Write([]byte(body)); err != nil {
		return err
	}
	return qw.Close()
}

func joinAddresses(list []Address) string {
	parts := make([]string, 0, len(list))
	for _, a := range list {
		if strings.TrimSpace(a.Email) != "" {
			parts = append(parts, a.String())
		}
	}
	return strings.Join(parts, ", ")
}

func encodeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}

// Package provider — 邮件发送的抽象与实现（pkg/mailer 的 provider 层）。
//
// 这里**不碰数据库、不碰业务**：只描述「一封邮件长什么样」与「怎么发出去、怎么分类失败」。
// 发信账号从哪来（mail_accounts 表）由 internal/module/mail 决定，本包不认识它。
package provider

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
)

// Address 邮件地址。
type Address struct {
	Name  string
	Email string
}

// String 返回 `Name <email>` 形式（Name 为空时只有地址）。
func (a Address) String() string {
	if strings.TrimSpace(a.Name) == "" {
		return a.Email
	}
	return fmt.Sprintf("%s <%s>", a.Name, a.Email)
}

// Attachment 附件。
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Message 一封待发送的邮件。
type Message struct {
	From    Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	ReplyTo *Address
	Subject string
	// HTML 与 Text 同时给出时按 multipart/alternative 发送（收件端自己选）。
	HTML string
	Text string
	// Headers 附加头（如 List-Unsubscribe —— 营销邮件的合规要求）。
	Headers     map[string]string
	Attachments []Attachment
}

// Recipients 返回全部收件人（含抄送 / 密送，用于抑制名单检查与日志）。
func (m *Message) Recipients() []string {
	out := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	for _, group := range [][]Address{m.To, m.Cc, m.Bcc} {
		for _, a := range group {
			if addr := strings.TrimSpace(a.Email); addr != "" {
				out = append(out, addr)
			}
		}
	}
	return out
}

// Result 发送结果。
type Result struct {
	Provider  string
	MessageID string
	Accepted  []string
	Rejected  []string
}

// Kind 错误分类。
//
// 分类不是给日志好看的，它直接决定三件事：
//
//	· Temporary     —— 可重试（连接超时 / 4xx），退避后重投；
//	· Permanent     —— **不可重试**，且应把地址加入抑制名单（5xx / 地址不存在）；
//	· Configuration —— 重试无意义，需人介入（认证失败 / 域名未验证 / 端口不对）。
//
// 学自 Notifuse 的 pkg/emailerror：把「怎么失败」变成可编程的结论，
// 而不是一句字符串让上层猜。
type Kind string

const (
	KindTemporary     Kind = "temporary"
	KindPermanent     Kind = "permanent"
	KindConfiguration Kind = "configuration"
)

// Error 带分类的发送错误。
type Error struct {
	Kind     Kind
	Provider string
	Code     string
	Message  string
	Err      error
}

// Error 实现 error。
func (e *Error) Error() string {
	parts := []string{}
	if e.Provider != "" {
		parts = append(parts, e.Provider)
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	parts = append(parts, e.Message)
	msg := strings.Join(parts, ": ")
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap 支持 errors.Is / errors.As 穿透到底层错误。
func (e *Error) Unwrap() error { return e.Err }

// IsPermanent 是否为不可重试的失败。
func (e *Error) IsPermanent() bool { return e != nil && e.Kind == KindPermanent }

// AsError 把任意错误转成带分类的 *Error（已经是就用原样）。
func AsError(providerName string, err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return classify(providerName, err)
}

// classify 按底层错误判定分类。
//
// 规则（SMTP 语义）：
//
//	· 带 5xx 代码 → Permanent（含 550 地址不存在 / 553 地址非法）；
//	· 带 4xx 代码 → Temporary（含 421 服务不可用 / 450 邮箱忙）；
//	· 认证 / 连接建立阶段的错误 → Configuration（重试一万次也没用）。
func classify(providerName string, err error) *Error {
	code, msg := smtpCode(err)
	switch {
	case code >= 500:
		return &Error{Kind: KindPermanent, Provider: providerName, Code: fmt.Sprint(code), Message: msg, Err: err}
	case code >= 400:
		return &Error{Kind: KindTemporary, Provider: providerName, Code: fmt.Sprint(code), Message: msg, Err: err}
	}
	return &Error{Kind: KindConfiguration, Provider: providerName, Message: msg, Err: err}
}

// smtpCode 尝试取出 SMTP 响应码（取不到返回 0）。
func smtpCode(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		return protoErr.Code, protoErr.Msg
	}
	return 0, err.Error()
}

// Sender 发送器抽象：SMTP 起步，SES / Mailgun / Postmark / SendGrid 按同一接口扩展。
type Sender interface {
	Send(ctx context.Context, msg *Message) (*Result, error)
	Name() string
}

package provider

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"testing"
)

func TestClassifySMTPSemantics(t *testing.T) {

	cases := []struct {
		code int
		want Kind
	}{
		{550, KindPermanent},
		{553, KindPermanent},
		{421, KindTemporary},
		{450, KindTemporary},
	}
	for _, tc := range cases {
		err := &textproto.Error{Code: tc.code, Msg: "x"}
		got := AsError("smtp", err)
		if got.Kind != tc.want {
			t.Fatalf("SMTP %d 应归类为 %s，实际 %s", tc.code, tc.want, got.Kind)
		}
	}

	got := AsError("smtp", errors.New("dial tcp: connection refused"))
	if got.Kind != KindConfiguration {
		t.Fatalf("连接类错误应归类为 configuration，实际 %s", got.Kind)
	}

	orig := &Error{Kind: KindPermanent, Message: "已分类"}
	if again := AsError("smtp", orig); again != orig {
		t.Fatalf("已分类的错误不该被重新包装")
	}
}

func TestPermanentErrorIsNotRetried(t *testing.T) {
	perm := AsError("smtp", &textproto.Error{Code: 550, Msg: "no such user"})
	if !perm.IsPermanent() {
		t.Fatalf("550 应判为不可重试")
	}
	temp := AsError("smtp", &textproto.Error{Code: 421, Msg: "try later"})
	if temp.IsPermanent() {
		t.Fatalf("421 不该判为不可重试（否则永远不会重投）")
	}
}

func TestMessageRecipientsIncludesAllGroups(t *testing.T) {
	m := &Message{
		To:  []Address{{Email: "a@example.com"}},
		Cc:  []Address{{Email: "b@example.com"}},
		Bcc: []Address{{Email: "c@example.com"}, {Email: "  "}},
	}
	got := m.Recipients()
	if len(got) != 3 {
		t.Fatalf("应收拢 3 个有效收件人（空白地址要跳过），实际 %v", got)
	}
}

func TestAddressString(t *testing.T) {
	if s := (Address{Name: "客服", Email: "a@b.com"}).String(); s != "客服 <a@b.com>" {
		t.Fatalf("带名字的格式不对: %q", s)
	}
	if s := (Address{Email: "a@b.com"}).String(); s != "a@b.com" {
		t.Fatalf("无名字时不该带尖括号: %q", s)
	}
}

func TestBuildMIMEPicksStructure(t *testing.T) {
	from := Address{Email: "from@example.com"}

	body, err := buildMIME(&Message{To: []Address{{Email: "to@example.com"}}, Subject: "hi", HTML: "<p>h</p>", Text: "h"}, from)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "multipart/alternative") {
		t.Fatalf("HTML+文本应产生 alternative 结构:\n%s", body)
	}

	body, err = buildMIME(&Message{
		To: []Address{{Email: "to@example.com"}}, Subject: "hi", HTML: "<p>h</p>",
		Attachments: []Attachment{{Filename: "a.txt", ContentType: "text/plain", Data: []byte("x")}},
	}, from)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "multipart/mixed") || !strings.Contains(s, "attachment; filename=\"a.txt\"") {
		t.Fatalf("有附件应产生 mixed 结构并带 Content-Disposition:\n%s", s)
	}

	body, err = buildMIME(&Message{To: []Address{{Email: "to@example.com"}}, Subject: "hi", Text: "plain"}, from)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Content-Type: text/plain") {
		t.Fatalf("纯文本邮件应直接是 text/plain:\n%s", body)
	}
}

func TestBuildMIMEEncodesNonASCIISubject(t *testing.T) {
	body, err := buildMIME(&Message{
		To: []Address{{Email: "to@example.com"}}, Subject: "验证码", Text: "x",
	}, Address{Email: "from@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)

	if !strings.Contains(s, "=?UTF-8?B?") {
		t.Fatalf("中文主题应做 RFC2047 编码:\n%s", s)
	}
	want := base64.StdEncoding.EncodeToString([]byte("验证码"))
	if !strings.Contains(s, want) {
		t.Fatalf("编码后的主题内容不对，期望含 %s", want)
	}
}

func TestNewSMTPSenderValidatesConfig(t *testing.T) {
	bad := []SMTPConfig{
		{Host: "", Port: 587, From: Address{Email: "a@b.com"}},
		{Host: "smtp.example.com", Port: 0, From: Address{Email: "a@b.com"}},
		{Host: "smtp.example.com", Port: 587, From: Address{}},
		{Host: "smtp.example.com", Port: 587, From: Address{Email: "a@b.com"}, Encryption: "pgp"},
	}
	for i, cfg := range bad {
		if _, err := NewSMTPSender(cfg); err == nil {
			t.Fatalf("第 %d 个非法配置应当报错: %+v", i, cfg)
		}
	}
	if _, err := NewSMTPSender(SMTPConfig{Host: "smtp.example.com", Port: 587, From: Address{Email: "a@b.com"}}); err != nil {
		t.Fatalf("合法配置不该报错: %v", err)
	}
}

func TestSendRejectsEmptyMessage(t *testing.T) {
	s, err := NewSMTPSender(SMTPConfig{Host: "smtp.example.com", Port: 587, From: Address{Email: "a@b.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(nil, nil); err == nil {
		t.Fatalf("空邮件应当直接拒绝（分类为配置问题，不必浪费一次连接）")
	} else {
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindConfiguration {
			t.Fatalf("空邮件的错误应归类为 configuration，实际 %v", err)
		}
	}
	if _, err = s.Send(nil, &Message{Subject: "no recipient"}); err == nil {
		t.Fatalf("无收件人应当直接拒绝")
	}
	_ = fmt.Sprint("")
}

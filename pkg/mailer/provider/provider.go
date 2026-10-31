package provider

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
)

type Address struct {
	Name  string
	Email string
}

func (a Address) String() string {
	if strings.TrimSpace(a.Name) == "" {
		return a.Email
	}
	return fmt.Sprintf("%s <%s>", a.Name, a.Email)
}

type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

type Message struct {
	From    Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	ReplyTo *Address
	Subject string

	HTML string
	Text string

	Headers     map[string]string
	Attachments []Attachment
}

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

type Result struct {
	Provider  string
	MessageID string
	Accepted  []string
	Rejected  []string
}

type Kind string

const (
	KindTemporary     Kind = "temporary"
	KindPermanent     Kind = "permanent"
	KindConfiguration Kind = "configuration"
)

type Error struct {
	Kind     Kind
	Provider string
	Code     string
	Message  string
	Err      error
}

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

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) IsPermanent() bool { return e != nil && e.Kind == KindPermanent }

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

type Sender interface {
	Send(ctx context.Context, msg *Message) (*Result, error)
	Name() string
}

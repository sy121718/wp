package jsonx

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
)

type RawMessage = jsontext.Value

type Number = json.Number

var ErrUnknownName = jsonv2.ErrUnknownName

type Options = jsonv2.Options

var v1Compat = jsonv2.JoinOptions(
	jsonv2.Deterministic(true),
	jsonv2.MatchCaseInsensitiveNames(true),
	jsonv2.FormatNilSliceAsNull(true),
	jsonv2.FormatNilMapAsNull(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.AllowDuplicateNames(true),
)

func withV1(opts ...jsonv2.Options) []jsonv2.Options {
	out := make([]jsonv2.Options, 0, len(opts)+1)
	out = append(out, v1Compat)
	out = append(out, opts...)
	return out
}

func RejectUnknownMembers(v bool) jsonv2.Options { return jsonv2.RejectUnknownMembers(v) }

func Marshal(v any, opts ...jsonv2.Options) ([]byte, error) {
	return jsonv2.Marshal(v, withV1(opts...)...)
}

func MarshalIndent(v any, prefix, indent string, opts ...jsonv2.Options) ([]byte, error) {
	raw, err := Marshal(v, opts...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := Indent(&buf, raw, prefix, indent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func MarshalWrite(wr io.Writer, v any, opts ...jsonv2.Options) error {
	b, err := Marshal(v, opts...)
	if err != nil {
		return err
	}
	_, err = wr.Write(b)
	return err
}

func Unmarshal(data []byte, v any, opts ...jsonv2.Options) error {
	return jsonv2.Unmarshal(data, v, withV1(opts...)...)
}

func UnmarshalRead(r io.Reader, v any, opts ...jsonv2.Options) error {
	return jsonv2.UnmarshalRead(r, v, withV1(opts...)...)
}

func Valid(b []byte) bool { return jsontext.Value(b).IsValid() }

func Compact(dst *bytes.Buffer, src []byte) error {
	v := jsontext.Value(bytes.Clone(src))
	if err := v.Compact(); err != nil {
		return err
	}
	_, err := dst.Write(v)
	return err
}

func Indent(dst *bytes.Buffer, src []byte, prefix, indent string) error {
	v := jsontext.Value(bytes.Clone(src))
	if err := v.Indent(jsontext.WithIndent(indent), jsontext.WithIndentPrefix(prefix)); err != nil {
		return err
	}
	_, err := dst.Write(v)
	return err
}

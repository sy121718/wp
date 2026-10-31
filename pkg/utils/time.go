package utils

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	LayoutSecond = "2006-01-02 15:04:05"

	LayoutDay = "2006-01-02"

	LayoutHour = "2006-01-02T15:00"

	LayoutJSON = "2006-01-02T15:04:05Z07:00"
)

type JSONTime time.Time

func NewJSONTime(t time.Time) JSONTime { return JSONTime(t) }

func NewJSONTimePtr(t *time.Time) *JSONTime {
	if t == nil {
		return nil
	}
	jt := JSONTime(*t)
	return &jt
}

func (t JSONTime) Time() time.Time { return time.Time(t) }

func (t *JSONTime) TimePtr() *time.Time {
	if t == nil {
		return nil
	}
	std := time.Time(*t)
	return &std
}

func (t *JSONTime) TimeOrZero() time.Time {
	if t == nil {
		return time.Time{}
	}
	return time.Time(*t)
}

func (t JSONTime) IsZero() bool { return time.Time(t).IsZero() }

func (t JSONTime) MarshalJSON() ([]byte, error) {
	std := time.Time(t)
	if std.IsZero() {
		return []byte("null"), nil
	}
	return []byte(strconv.FormatInt(std.UnixMilli(), 10)), nil
}

func (t *JSONTime) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` || s == "" {
		*t = JSONTime{}
		return nil
	}
	s = strings.Trim(s, "\"")

	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		*t = JSONTime(time.UnixMilli(ms))
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, LayoutSecond, LayoutDay} {
		if parsed, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			*t = JSONTime(parsed)
			return nil
		}
	}
	return fmt.Errorf("时间格式无法解析: %q（支持 RFC3339 / %s / %s）", s, LayoutSecond, LayoutDay)
}

func (t JSONTime) String() string {
	std := time.Time(t)
	if std.IsZero() {
		return ""
	}
	return std.Format(LayoutSecond)
}

func (t JSONTime) Value() (driver.Value, error) {
	std := time.Time(t)
	if std.IsZero() {
		return nil, nil
	}
	return std, nil
}

func (t *JSONTime) Scan(v any) error {
	if v == nil {
		*t = JSONTime{}
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		*t = JSONTime(val)
		return nil
	case []byte:
		return t.UnmarshalJSON(bytes.TrimSpace(val))
	case string:
		return t.UnmarshalJSON([]byte(val))
	}
	return fmt.Errorf("无法把 %T 扫描成 JSONTime", v)
}

func (t JSONTime) Second() JSONTime { return JSONTime(time.Time(t).Truncate(time.Second)) }

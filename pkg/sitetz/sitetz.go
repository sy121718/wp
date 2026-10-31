package sitetz

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const envKey = "GO_WP_SITE_TIMEZONE"

var (
	once sync.Once
	loc  *time.Location
)

func Location() *time.Location {
	once.Do(func() {
		raw := strings.TrimSpace(os.Getenv(envKey))
		if raw != "" {
			if parsed, err := time.LoadLocation(raw); err == nil {
				loc = parsed
				return
			}
		}
		loc = time.Local
	})
	return loc
}

func Name() string {
	return Location().String()
}

func ParseDay(raw string) (t time.Time, err error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), Location())
}

var datetimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

func ParseDateTime(raw string) (t time.Time, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, errors.New("时间不能为空")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, perr := time.Parse(layout, s); perr == nil {
			return parsed, nil
		}
	}
	for _, layout := range datetimeLayouts {
		if parsed, perr := time.ParseInLocation(layout, s, Location()); perr == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("时间格式无法解析: %q（支持 YYYY-MM-DD HH:MM[:SS] 与带时区的 RFC3339）", raw)
}

func FormatDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(Location()).Format("2006-01-02T15:04")
}

func ResetForTest() {
	once = sync.Once{}
	loc = nil
}

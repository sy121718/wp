package sysconfigservice

import (
	"testing"
	"time"
)

func TestCountryLabelEntryValidity(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		expires time.Time
		want    bool
	}{
		{name: "未过期可用", expires: now.Add(time.Minute), want: true},
		{name: "已过期失效", expires: now.Add(-time.Second), want: false},

		{name: "正好到期按失效处理", expires: now, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := countryLabelEntry{expires: tt.expires}
			if got := entry.valid(now); got != tt.want {
				t.Fatalf("valid(expires=%v, now=%v) = %v，期望 %v", tt.expires, now, got, tt.want)
			}
		})
	}
}

package utils

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// 流水表主键必须是 v7：一旦有人换回 uuid.NewString()，页分裂的写放大就回来了。
func TestNewTimeOrderedIDIsV7AndFresh(t *testing.T) {
	id := NewTimeOrderedID()
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("生成的 id 不是合法 uuid：%q（%v）", id, err)
	}
	if got := parsed.Version(); got != 7 {
		t.Fatalf("期望 UUIDv7，实际 version=%d，id=%s", got, id)
	}
	// v7 前 48 位是毫秒时间戳 —— 按 RFC 9562 布局直接解析，不依赖库对 Time() 的实现
	ms := int64(parsed[0])<<40 | int64(parsed[1])<<32 | int64(parsed[2])<<24 |
		int64(parsed[3])<<16 | int64(parsed[4])<<8 | int64(parsed[5])
	if d := time.Since(time.UnixMilli(ms)); d > time.Minute || d < -time.Minute {
		t.Fatalf("v7 时间前缀与当前时间偏差过大：%v", d)
	}
}

func TestNewTimeOrderedIDUnique(t *testing.T) {
	const n = 5000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewTimeOrderedID()
		if _, dup := seen[id]; dup {
			t.Fatalf("第 %d 次生成重复 id：%s", i, id)
		}
		seen[id] = struct{}{}
	}
}

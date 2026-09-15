package mediaservice

import (
	"testing"
	"time"
)

func TestMediaCursorRoundTrip(t *testing.T) {
	wantTime := time.Date(2026, 9, 15, 8, 31, 47, 123456789, time.FixedZone("CST", 8*60*60))
	wantID := uint64(42)
	cursor := encodeMediaCursor(wantTime, wantID)
	gotTime, gotID, err := decodeMediaCursor(cursor)
	if err != nil {
		t.Fatalf("游标解码失败: %v", err)
	}
	if !gotTime.Equal(wantTime) || gotID != wantID {
		t.Fatalf("游标往返不一致: got=%s/%d want=%s/%d", gotTime, gotID, wantTime, wantID)
	}
}

func TestDecodeMediaCursorRejectsInvalid(t *testing.T) {
	for _, cursor := range []string{"", "bad", "YWJj", "MjAyNi0wOS0xNVQwODozMTo0N1p8MA"} {
		if _, _, err := decodeMediaCursor(cursor); err == nil {
			t.Errorf("无效游标应报错: %q", cursor)
		}
	}
}

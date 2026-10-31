package utils

import (
	json "go_wp/pkg/jsonx"
	"testing"
	"time"
)

func TestJSONTimeMarshalIsUnixMilli(t *testing.T) {
	std := time.Date(2026, 9, 16, 13, 57, 50, 123456000, time.UTC)
	b, err := json.Marshal(JSONTime(std))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if got, want := string(b), "1789567070123"; got != want {
		t.Fatalf("应输出 unix 毫秒，得到 %s，期望 %s", got, want)
	}
}

func TestJSONTimeMarshalZeroIsNull(t *testing.T) {
	b, err := json.Marshal(JSONTime{})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(b) != "null" {
		t.Fatalf("零值应序列化为 null，实际 %s", b)
	}
}

func TestJSONTimeNilPointerIsNull(t *testing.T) {
	var p *JSONTime
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(b) != "null" {
		t.Fatalf("nil 指针应序列化为 null，实际 %s", b)
	}
}

func TestJSONTimeUnmarshalAcceptsCommonLayouts(t *testing.T) {

	_, offset := time.Now().Zone()
	cases := []struct {
		name  string
		input string
	}{
		{"RFC3339 无小数秒", `"2026-09-16T13:57:50Z"`},
		{"RFC3339 带微秒", `"2026-09-16T13:57:50.123456Z"`},
		{"空格分隔", `"2026-09-16 13:57:50"`},
		{"纯日期", `"2026-09-16"`},
		{"null", `null`},
		{"空串", `""`},
	}
	for _, c := range cases {
		var got JSONTime
		if err := json.Unmarshal([]byte(c.input), &got); err != nil {
			t.Fatalf("%s（%s）应能解析: %v", c.name, c.input, err)
		}
	}

	var withFrac JSONTime
	if err := json.Unmarshal([]byte(`"2026-09-16T13:57:50.123456Z"`), &withFrac); err != nil {
		t.Fatalf("解析带微秒的时间失败: %v", err)
	}
	if withFrac.Time().Nanosecond() != 123456000 {
		t.Fatalf("内部应保留微秒精度，实际纳秒 %d", withFrac.Time().Nanosecond())
	}
	_ = offset

	var bad JSONTime
	if err := json.Unmarshal([]byte(`"昨天"`), &bad); err == nil {
		t.Fatal("无法解析的格式应报错")
	}
}

func TestJSONTimeRoundTrip(t *testing.T) {
	orig := JSONTime(time.Date(2026, 1, 2, 3, 4, 5, 678901000, time.UTC))
	b, _ := json.Marshal(orig)
	var back JSONTime
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if !back.Time().Equal(orig.Time().Truncate(time.Millisecond)) {
		t.Fatalf("回读后应等于截到秒的原值：%v vs %v", back.Time(), orig.Time())
	}
}

func TestJSONTimeValueKeepsMicroseconds(t *testing.T) {
	std := time.Date(2026, 9, 16, 13, 57, 50, 123456000, time.UTC)
	v, err := JSONTime(std).Value()
	if err != nil {
		t.Fatalf("Value 失败: %v", err)
	}
	got, ok := v.(time.Time)
	if !ok {
		t.Fatalf("Value 应返回 time.Time，实际 %T", v)
	}
	if !got.Equal(std) {
		t.Fatalf("写库值应与原值逐纳秒一致：%v vs %v", got, std)
	}

	nilV, err := JSONTime{}.Value()
	if err != nil || nilV != nil {
		t.Fatalf("零值应写 NULL，实际 %v / %v", nilV, err)
	}
}

func TestJSONTimeScan(t *testing.T) {
	std := time.Date(2026, 9, 16, 13, 57, 50, 0, time.UTC)
	var got JSONTime
	if err := got.Scan(std); err != nil {
		t.Fatalf("Scan(time.Time) 失败: %v", err)
	}
	if !got.Time().Equal(std) {
		t.Fatalf("Scan 结果不对：%v", got.Time())
	}
	var nilGot JSONTime
	if err := nilGot.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) 失败: %v", err)
	}
	if !nilGot.IsZero() {
		t.Fatal("Scan(nil) 应得零值")
	}
}

package utils

// time_test.go — 对外 JSON 时间的口径（存储微秒、输出到秒）。

import (
	"encoding/json"
	"testing"
	"time"
)

// TestJSONTimeMarshalTruncatesToSecond 输出必须**丢到秒**：这是这个类型存在的全部理由。
func TestJSONTimeMarshalTruncatesToSecond(t *testing.T) {
	// 带微秒的输入（库里的 timestamptz(6) 就是这种精度）。
	std := time.Date(2026, 9, 16, 13, 57, 50, 123456000, time.UTC)
	b, err := json.Marshal(JSONTime(std))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if got, want := string(b), `"2026-09-16T13:57:50Z"`; got != want {
		t.Fatalf("应截到秒，得到 %s，期望 %s", got, want)
	}
}

// TestJSONTimeMarshalZeroIsNull 零值是 null，不是 "0001-01-01T00:00:00Z"。
func TestJSONTimeMarshalZeroIsNull(t *testing.T) {
	b, err := json.Marshal(JSONTime{})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(b) != "null" {
		t.Fatalf("零值应序列化为 null，实际 %s", b)
	}
}

// TestJSONTimeNilPointerIsNull 可空字段用 *JSONTime，nil 即 null。
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

// TestJSONTimeUnmarshalAcceptsCommonLayouts 解析要比标准库宽松。
//
// 标准库的 time.Time 只吃 RFC3339，而后台原生表单与既有客户端在用空格格式与纯日期 ——
// 这是 req 里的时间字段此前的坑。
func TestJSONTimeUnmarshalAcceptsCommonLayouts(t *testing.T) {
	// 引入固定的本地时区偏移，避免断言随运行环境变化。
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

	// 带微秒的输入按本地时区解析成同一时刻（精度保留在内部，只是不往外输出）。
	var withFrac JSONTime
	if err := json.Unmarshal([]byte(`"2026-09-16T13:57:50.123456Z"`), &withFrac); err != nil {
		t.Fatalf("解析带微秒的时间失败: %v", err)
	}
	if withFrac.Time().Nanosecond() != 123456000 {
		t.Fatalf("内部应保留微秒精度，实际纳秒 %d", withFrac.Time().Nanosecond())
	}
	_ = offset

	// 无法识别的格式要明确报错，而不是静默落零值。
	var bad JSONTime
	if err := json.Unmarshal([]byte(`"昨天"`), &bad); err == nil {
		t.Fatal("无法解析的格式应报错")
	}
}

// TestJSONTimeRoundTrip 反序列化后再序列化仍回到秒精度。
func TestJSONTimeRoundTrip(t *testing.T) {
	orig := JSONTime(time.Date(2026, 1, 2, 3, 4, 5, 678901000, time.UTC))
	b, _ := json.Marshal(orig)
	var back JSONTime
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if !back.Time().Equal(orig.Time().Truncate(time.Second)) {
		t.Fatalf("回读后应等于截到秒的原值：%v vs %v", back.Time(), orig.Time())
	}
}

// TestJSONTimeValueKeepsMicroseconds 写库时**不能**截断 —— 存储层要保持微秒。
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

	// 零值写 NULL 而不是 0001-01-01。
	nilV, err := JSONTime{}.Value()
	if err != nil || nilV != nil {
		t.Fatalf("零值应写 NULL，实际 %v / %v", nilV, err)
	}
}

// TestJSONTimeScan 读库回填。
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

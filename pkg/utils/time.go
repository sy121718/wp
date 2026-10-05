package utils

// time.go — 对外 JSON 时间的统一口径（DB-019 精度收口的对外一半）。
//
// 库里的时间列是 **timestamptz(6)**：带时区、微秒精度，全库 199 列口径一致
//（迁移 205 统一列名、212 收掉最后 4 个 bigint 秒）。存储层不需要动。
//
// 需要收的是**对外输出**：Go 的 time.Time 默认按 RFC3339Nano 序列化
//（2026-09-16T13:57:50.123456+08:00），把内部的存储精度泄漏到了协议上 ——
//   · 同一秒内的两次写入在 JSON 里看起来不同，前端做秒级比较 / 分组要自己截断；
//   · 响应体积无谓变大（每条记录多 7~10 字节，列表接口乘起来很可观）；
//   · 协议跟着存储走 —— 将来若调整存储精度，对外契约会跟着变，而契约不该随存储变。
//
// 所以对外统一用 JSONTime：**存储保持微秒，输出到秒**。
// 需要更高精度时用 Time() 取回标准 time.Time 自行格式化，不要在 JSON 里透出。

import (
	"bytes"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// 时间布局常量。
//
// 项目里此前有十余处硬编码 "2006-01-02 15:04:05"（admin / media / masterdata /
// countdown 各写一遍），这里收成一份 —— 换口径时只改一处。
const (
	// LayoutSecond 到秒的空格分隔展示格式（后台列表、导出、日志用）。
	LayoutSecond = "2006-01-02 15:04:05"
	// LayoutDay 纯日期。
	LayoutDay = "2006-01-02"
	// LayoutHour 到小时的桶键（趋势图按小时聚合时用）。
	//
	// 用 `T` 分隔而不是空格：它是**机器用的桶键**，不是给人看的展示格式 ——
	// 带空格的键在 URL / 日志 / JSON 里都要转义，而桶键会流经这些地方。
	// 展示层再按粒度切成「01-02」「10:00」这类人读标签。
	LayoutHour = "2006-01-02T15:00"
	// LayoutJSON 对外 JSON 的格式：RFC3339 到秒（带时区偏移，前端 new Date() 可直接解析）。
	LayoutJSON = "2006-01-02T15:04:05Z07:00"
)

// JSONTime 对外的 JSON 时间：序列化**默认只到秒**。
//
// 零值序列化为 null（而不是 "0001-01-01T00:00:00Z"）—— 后者是 Go 零值的实现细节，
// 出现在 JSON 里只会诱使前端把它当真实时间处理。可空字段直接用 *JSONTime（nil 即 null）。
type JSONTime time.Time

// NewJSONTime 从标准 time.Time 构造。
func NewJSONTime(t time.Time) JSONTime { return JSONTime(t) }

// NewJSONTimePtr 从 *time.Time 构造；nil 进 nil 出（可空字段用）。
//
// 为什么不直接 `(*JSONTime)(p)`：那需要先判空再取地址，每个赋值点写三行；
// dto 的可空时间字段很多（支付时间 / 完成时间 / 上次登录……），收成一处。
func NewJSONTimePtr(t *time.Time) *JSONTime {
	if t == nil {
		return nil
	}
	jt := JSONTime(*t)
	return &jt
}

// Time 取回标准 time.Time（需要更高精度或做时间运算时用）。
func (t JSONTime) Time() time.Time { return time.Time(t) }

// TimePtr 取回 *time.Time；nil 进 nil 出（把 dto 的可空时间传回 model 时用）。
func (t *JSONTime) TimePtr() *time.Time {
	if t == nil {
		return nil
	}
	std := time.Time(*t)
	return &std
}

// TimeOrZero 取回 time.Time，nil 时给零值（不可空的字段用）。
func (t *JSONTime) TimeOrZero() time.Time {
	if t == nil {
		return time.Time{}
	}
	return time.Time(*t)
}

// IsZero 是否为零值。
func (t JSONTime) IsZero() bool { return time.Time(t).IsZero() }

// MarshalJSON 按 RFC3339 截到秒输出；零值给 null。
func (t JSONTime) MarshalJSON() ([]byte, error) {
	std := time.Time(t)
	if std.IsZero() {
		return []byte("null"), nil
	}
	return []byte("\"" + std.Format(LayoutJSON) + "\""), nil
}

// UnmarshalJSON 解析外部传入的时间。
//
// 比标准 time.Time 宽松：除 RFC3339（带或不带小数秒）外，还接受空格分隔的
// "2006-01-02 15:04:05" 与纯日期 "2006-01-02" —— 后台原生表单与既有客户端在用，
// 标准库解析这两个会直接报错（此前 req 里的时间字段就有这个坑）。
// 空串与 null 都当零值，不报错。
func (t *JSONTime) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` || s == "" {
		*t = JSONTime{}
		return nil
	}
	s = strings.Trim(s, "\"")
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, LayoutSecond, LayoutDay} {
		if parsed, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			*t = JSONTime(parsed)
			return nil
		}
	}
	return fmt.Errorf("时间格式无法解析: %q（支持 RFC3339 / %s / %s）", s, LayoutSecond, LayoutDay)
}

// String 实现 fmt.Stringer（到秒）。
func (t JSONTime) String() string {
	std := time.Time(t)
	if std.IsZero() {
		return ""
	}
	return std.Format(LayoutSecond)
}

// Value 实现 driver.Valuer（写库时转标准 time.Time，保持 timestamptz 的微秒精度）。
func (t JSONTime) Value() (driver.Value, error) {
	std := time.Time(t)
	if std.IsZero() {
		return nil, nil
	}
	return std, nil
}

// Scan 实现 sql.Scanner（读库时沿用驱动的 time.Time）。
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

// Second 把时间截断到秒（写库前要用秒钟精度比较时用）。
func (t JSONTime) Second() JSONTime { return JSONTime(time.Time(t).Truncate(time.Second)) }

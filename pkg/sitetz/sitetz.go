// Package sitetz 站点时区的单一声明点（审计 TX-011 / DB-018）。
//
// 系统里的时间有两类用途，口径不同、必须分开：
//
//   - **存储与比较**：一律绝对时刻（列是 timestamptz，Go 侧用 UTC 计算）；
//   - **面向人的输入输出**：按站点时区理解 —— 「筛 9 月 14 日」指的是站点所在地的那一天，
//     「活动 10:00 开始」指的也是那个时区的 10:00。
//
// 本包只负责后者。默认跟随服务器时区（与改造前行为一致，不改变任何既有语义），
// 可用 GO_WP_SITE_TIMEZONE 固定为 IANA 名（如 Asia/Shanghai）：部署时固定下来之后，
// 服务器时区就不再影响任何面向人的时间口径。
//
// 为什么值得单独一个包：口径散落在各处时，「这条筛选按谁的时区算」只能靠读代码猜；
// 收敛到一处之后，改口径是改一个地方，而不是去全仓找 time.Local。
package sitetz

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// envKey 站点时区的环境变量名。
const envKey = "GO_WP_SITE_TIMEZONE"

var (
	once sync.Once
	loc  *time.Location
)

// Location 站点时区。
//
// 解析顺序：环境变量指定的 IANA 名 → 服务器本地时区。
// 环境变量写了但解析不出来时**回退本地时区并记住**（不 panic）：
// 一个拼错的时区名不该让整个站点起不来，而配置错误会在启动日志里由 Name() 体现。
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

// Name 当前生效的时区名（日志与诊断用：一眼看出站点按哪个时区解释时间）。
func Name() string {
	return Location().String()
}

// ParseDay 按站点时区解析 YYYY-MM-DD（后台筛选日期用）。
func ParseDay(raw string) (t time.Time, err error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), Location())
}

// datetimeLayouts 站点时区下的「日期 + 时刻」形态（按顺序尝试）。
//
// 秒可选、分隔符空格与 T 都收：后台表单（datetime-local 给 T 分隔、不带秒）与手输
// （习惯空格分隔、带秒）都要能用。纯日期放最后，按当日 00:00 —— 与 ParseDay 同义。
var datetimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// ParseDateTime 按站点时区解析「日期 + 时刻」（审计 TX-011 的口径：面向人的输入按站点时区理解）。
//
// 与 ParseDay 的分工：那条只到日粒度（筛选「9 月 14 日」）；排定「几点几分上线」需要到时刻。
//
// **带偏移量的 RFC3339 是例外**：那是绝对时刻，按它自带的偏移解析、不再套站点时区 ——
// 调用方明确给了绝对时刻时替它改时区，等于静默把到点时间挪几个时区。
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

// FormatDateTime 把绝对时刻写成站点时区下的 "2006-01-02T15:04"（后台 datetime-local 的回显形态）。
//
// 与 ParseDateTime 成对：读侧输出的字面量必须能被写侧原样解析回来（表单往返不加时区偏移时
// 差一截）。零值返回空串 —— 让模板渲染出一个空输入框，而不是 0001-01-01。
func FormatDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(Location()).Format("2006-01-02T15:04")
}

// ResetForTest 重置缓存（测试改环境变量后调用）。
func ResetForTest() {
	once = sync.Once{}
	loc = nil
}

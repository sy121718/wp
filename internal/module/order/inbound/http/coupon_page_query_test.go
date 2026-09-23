package orderhttp

// coupon_page_query_test.go — 时间窗控件的提交形态与服务端解析口径之间的那一处转换。
//
// 背景（审计 02-O §3 任务 1）：优惠码的四个时间窗输入框由 <input type="text"> 换成原生
// <input type="datetime-local">。控件提交的是它自己的 value 形态 ——「2026-01-01T09:00」（带 T），
// 而优惠码服务端的解析布局只有「2006-01-02」/「2006-01-02 15:04(:05)」。
// 不在表单解析归口转换一次，就是**表单自己生成的格式自己都不收**：控件换对了、保存反而全废。
//
// 本文件钉两件事：
//  1. couponFormTimeValue 的转换表（含「不该动」的几种形态 —— 空值、纯日期、空格写法、
//     畸形串都要原样透传，归一化不能顺手吞掉或改写它们）；
//  2. couponSaveReqFromForm **真的接上了**这一转换（函数写对了但没接上是典型缺陷）。
//
// 「归一化之后的形态真的落得了库」这一环由 public/test/order/feature/coupon_window_timezone_test.go
// 的 TestCouponWindowAcceptsDateTimePickerForm 承接（真实 service + 真实数据库，用的是同一个字面量）。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// couponFormCtx 构造带 URL 编码表单体的 POST 上下文（与浏览器提交后台表单同形）。
func couponFormCtx(t *testing.T, form url.Values) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/coupons/create", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c
}

// TestCouponFormTimeValueConvertsDateTimePickerShape 控件形态 → 服务端布局。
func TestCouponFormTimeValueConvertsDateTimePickerShape(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"datetime-local 到分（控件默认 step=60 的形态）", "2026-01-01T09:00", "2026-01-01 09:00"},
		{"datetime-local 带秒（手输或 step 放开时）", "2026-01-01T09:00:30", "2026-01-01 09:00:30"},
		{"首尾空白一并去掉", "  2026-01-01T09:00  ", "2026-01-01 09:00"},
		{"结束侧同样是控件形态", "2026-01-31T23:59", "2026-01-31 23:59"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := couponFormTimeValue(tc.raw); got != tc.want {
				t.Fatalf("归一化结果应为 %q，实际 %q", tc.want, got)
			}
		})
	}
}

// TestCouponFormTimeValueKeepsEveryOtherShapeUntouched 归一化只认控件形态，其余原样透传。
//
// 这几种形态各自都有活着的来源，被「顺手统一」掉就是静默的数据改写或静默的拒绝：
//   - 空串 = 不限（service 的语义），改成任何非空值都会凭空造出一个时间窗；
//   - 纯日期 = 老客户端与「不支持 datetime-local 的浏览器退化成文本框」时的输入；
//   - 空格写法 = 换控件之前的所有客户端仍在用的写法；
//   - 畸形串 = 必须原样交给 service 拒绝（在这儿「修正」会让用户拿到一张他没配置过的券）。
func TestCouponFormTimeValueKeepsEveryOtherShapeUntouched(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		"2026-01-01",
		"2026-01-01 09:00",
		"2026-01-01 09:00:30",
		"2026/01/01 09:00",
		"2026-01-01T09:00+08:00",
	} {
		want := strings.TrimSpace(raw)
		if got := couponFormTimeValue(raw); got != want {
			t.Fatalf("%q 不该被改写：期望 %q，实际 %q", raw, want, got)
		}
	}
}

// TestCouponSaveReqFromFormNormalizesWindowFields 表单取值真的走了归一化（两处都要）。
func TestCouponSaveReqFromFormNormalizesWindowFields(t *testing.T) {
	c := couponFormCtx(t, url.Values{
		"code":     {"SAVE20"},
		"name":     {"双十一全场券"},
		"startsAt": {"2026-01-01T09:00"},
		"endsAt":   {"2026-01-31T23:59"},
	})

	req := couponSaveReqFromForm(c)
	if req.StartsAt != "2026-01-01 09:00" {
		t.Fatalf("startsAt 应归一化成服务端布局，实际 %q —— 归一化没接上表单取值就是「换了控件保存不了」",
			req.StartsAt)
	}
	if req.EndsAt != "2026-01-31 23:59" {
		t.Fatalf("endsAt 应归一化成服务端布局，实际 %q", req.EndsAt)
	}
}

// TestCouponFormTimeShapesTheControlCanActuallyHold 回填值必须是控件能持住、且能通过校验的形态。
//
// 三条都是实测出来的约束（Chrome 153，见本批的浏览器夹具结论）：
//   - `<input type="datetime-local">` 的 value 规范化只认带 T 的写法；**纯日期会被清成空串**，
//     所以回填必须含时刻，否则运营打开抽屉看到的是「时间窗没了」（库里其实有值）；
//   - 秒非 0 时必须输出到秒，且模板那边对应输入框要带 step="1"：默认 step=60 时 `…:00:30`
//     会被判 stepMismatch，**表单根本提交不了**（浏览器拦在提交前，服务端一行日志都没有）；
//   - 亚秒丢弃：控件无法安全表达微秒，而券的生效窗口精确到秒已经过头了。
func TestCouponFormTimeShapesTheControlCanActuallyHold(t *testing.T) {
	parse := func(s string) *time.Time {
		t.Helper()
		// 按**本地时区**解析：couponFormTime 输出的是 at.Local() 的形态（控件按浏览器本地
		// 时区显示/提交），用例与它同一口径，换服务器时区时断言依然成立。
		at, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local)
		if err != nil {
			t.Fatalf("构造用例时间失败: %v", err)
		}
		return &at
	}

	cases := []struct {
		name string
		at   *time.Time
		want string
	}{
		{"nil = 不限", nil, ""},
		{"整分不带秒（大多数券）", parse("2026-01-01T09:00:00"), "2026-01-01T09:00"},
		{"带秒时保留秒（配合 step=\"1\" 才可提交）", parse("2026-01-01T09:00:30"), "2026-01-01T09:00:30"},
		{"亚秒丢弃", parse("2026-01-01T09:00:30.123456"), "2026-01-01T09:00:30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := couponFormTime(tc.at); got != tc.want {
				t.Fatalf("回填值应为 %q，实际 %q", tc.want, got)
			}
			// 回填值的形态必须真能被控件接受（含时刻）；纯日期形态会被清成空串。
			if tc.want != "" && !strings.Contains(tc.want, "T") {
				t.Fatalf("回填值 %q 缺 T —— datetime-local 会把纯日期形态清成空串", tc.want)
			}
		})
	}
}

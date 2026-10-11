package reqparams

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIsHXRequestHeaderMatrix(t *testing.T) {
	if IsHXRequest(nil) {
		t.Fatal("nil context matched")
	}
	for _, tc := range []struct {
		header string
		want   bool
	}{
		{"", false}, {"true", true}, {" TRUE \t", true},
		{"false", false}, {"1", false}, {"true,false", false},
	} {
		t.Run(tc.header, func(t *testing.T) {
			c := reqCtx(t, http.MethodGet, "/probe", "")
			c.Request.Header.Set("HX-Request", tc.header)
			if got := IsHXRequest(c); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// 这批用例是 task-96（自 internal/shell 迁入）的**等价性证据**：迁移只搬实现、不改语义，
// 所以边界值逐条钉死 —— 每条都对应调用方已经依赖的一个口径，改它就是改所有页面的入参行为。
//
// 为什么这么细：「非法输入降级为零值」这类口径在页面上是**静默**的（解析失败不报错，
// 只是筛选项变成默认值），一旦被「顺手修正」，没有任何页面会报错，只会有人发现筛选失灵。

func reqCtx(t *testing.T, method, target, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if body == "" {
		c.Request = httptest.NewRequest(method, target, nil)
		return c
	}
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c
}

func TestFieldValueTrimsAndDefaultsEmpty(t *testing.T) {
	c := reqCtx(t, http.MethodPost, "/x", "name=%20%20%E5%95%86%E5%93%81%20&empty=+")
	if got := FieldValue(c, "name"); got != "商品" {
		t.Errorf("FieldValue 应去掉首尾空白，got %q", got)
	}
	if got := FieldValue(c, "empty"); got != "" {
		t.Errorf("全空白应收敛为空串，got %q", got)
	}
	if got := FieldValue(c, "missing"); got != "" {
		t.Errorf("缺键应返回空串，got %q", got)
	}
}

func TestParseUintDegradesToZero(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"", 0},
		{"  ", 0},
		{"42", 42},
		{" 42 ", 42},
		{"-1", 0},    // 负数不是非负整数
		{"abc", 0},   // 非数字
		{"12abc", 0}, // 尾部杂质
		// 溢出**不是 0**：strconv.ParseUint 在 ErrRange 时返回被夹到 MaxUint64 的值，
		// 而本函数忽略错误直接返回它 —— 这是原 shell 实现的口径，迁移逐字保留。
		// 顺手「修」成 0 是行为变更（超长 id 从「查不到」变成「id=0」）。
		{"18446744073709551616", 18446744073709551615},
	}
	for _, tc := range cases {
		if got := ParseUint(tc.in); got != tc.want {
			t.Errorf("ParseUint(%q)=%d，want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseStatusKeepsSign(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{{"", 0}, {"  ", 0}, {"3", 3}, {" 3 ", 3}, {"-1", -1}, {"abc", 0}} {
		if got := ParseStatus(tc.in); got != tc.want {
			t.Errorf("ParseStatus(%q)=%d，want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseStatusPtrDistinguishesMissingFromInvalid(t *testing.T) {
	if got := ParseStatusPtr(""); got != nil {
		t.Errorf("空串＝未传，必须是 nil，got %v", *got)
	}
	if got := ParseStatusPtr("  "); got != nil {
		t.Errorf("全空白＝未传，必须是 nil，got %v", *got)
	}
	if got := ParseStatusPtr("0"); got == nil || *got != 0 {
		t.Errorf("显式 0 必须是非 nil 的 0，got %v", got)
	}
	if got := ParseStatusPtr(" 2 "); got == nil || *got != 2 {
		t.Errorf("带空白应 trim 后解析，got %v", got)
	}
	// 「传了但非法」沿用老口径：非 nil 的 0（不是 nil —— 那会被读成「未传，默认启用」）。
	got := ParseStatusPtr("abc")
	if got == nil {
		t.Fatal("非法输入不能退化成 nil：调用方会把 nil 读成「未传」而默认启用")
	}
	if *got != 0 {
		t.Errorf("非法输入应降级为 0，got %d", *got)
	}
}

func TestPageParamsDefaultsAndClamps(t *testing.T) {
	cases := []struct {
		query       string
		wantPage    int
		wantLimit   int
		description string
	}{
		{"", 1, 20, "全缺省"},
		{"?page=3&limit=50", 3, 50, "正常值"},
		{"?page=0&limit=0", 1, 20, "0 走缺省"},
		{"?page=-2&limit=-5", 1, 20, "负数走缺省"},
		{"?page=abc&limit=xyz", 1, 20, "非法走缺省"},
		{"?page=2&limit=1000", 2, 100, "上限 100 收口"},
		{"?page=2&limit=101", 2, 100, "101 也收口到 100"},
		{"?page=2&limit=100", 2, 100, "恰好 100 不收口"},
	}
	for _, tc := range cases {
		c := reqCtx(t, http.MethodGet, "/x"+tc.query, "")
		page, limit := PageParams(c)
		if page != tc.wantPage || limit != tc.wantLimit {
			t.Errorf("%s：PageParams(%q)=(%d,%d)，want (%d,%d)",
				tc.description, tc.query, page, limit, tc.wantPage, tc.wantLimit)
		}
	}
}

func TestFilterBaseURLKeepsOnlyNonEmptyValues(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		filters map[string]string
		want    string
	}{
		{"全空", "/admin/coupons", map[string]string{"project": "", "status": ""}, "/admin/coupons"},
		{"nil map", "/admin/coupons", nil, "/admin/coupons"},
		{"单个非空", "/admin/coupons", map[string]string{"status": "1"}, "/admin/coupons?status=1"},
		{"键有序", "/admin/coupons", map[string]string{"b": "2", "a": "1"}, "/admin/coupons?a=1&b=2"},
		{"值转义", "/admin/coupons", map[string]string{"q": "a b&c"}, "/admin/coupons?q=a+b%26c"},
		{"空与非空混合", "/admin/coupons", map[string]string{"project": "p1", "keyword": ""}, "/admin/coupons?project=p1"},
	}
	for _, tc := range cases {
		if got := FilterBaseURL(tc.path, tc.filters); got != tc.want {
			t.Errorf("%s：FilterBaseURL(%q, %v)=%q，want %q", tc.name, tc.path, tc.filters, got, tc.want)
		}
	}
}

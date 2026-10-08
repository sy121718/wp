package shell

// notice_test.go — 读侧回执文案的受控形状判定（纯逻辑，不碰数据库）。
//
// 守的是三件事，每一件出错时都是**静默**的：
//
//  1. 数字归一 —— 批量结论文案里只有计数变化；归一失效会让「已删除 3 个块。」
//     连自己写侧声明的模板都匹配不上，运营看到的从具体结论退化成归口文案；
//  2. 未命中必须为空 —— 判定过宽的后果是「任何人手拼 ?err=任意文案 都能伪造系统提示」，
//     这正是本批要堵的那条路；
//  3. 语言回跳的收敛 —— 只允许站内相对路径且 ≤ langRedirectMaxBytes，
//     超出一条长 query 会把隐藏域撑成几 KB（并让 /admin/lang 302 到超长 URL）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func noticeCtx(t *testing.T, target string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}

func TestNormalizeNoticeDigits(t *testing.T) {
	cases := map[string]string{
		"已删除 3 个块。":           "已删除 0 个块。",
		"已删除 12 个，7 个未删除。":    "已删除 0 个，0 个未删除。",
		"没有数字":                "没有数字",
		"一次最多操作 200 项，当前 5 项": "一次最多操作 0 项，当前 0 项",
	}
	for raw, want := range cases {
		if got := NormalizeNoticeDigits(raw); got != want {
			t.Fatalf("NormalizeNoticeDigits(%q)=%q，want %q", raw, got, want)
		}
	}
}

func TestNoticeTemplate(t *testing.T) {
	if got := NoticeTemplate("已删除 %d 个块。"); got != "已删除 0 个块。" {
		t.Fatalf("模板归一失败: %q", got)
	}
	if got := NoticeTemplate("一次最多操作 %s 项，当前 %s 项"); got != "一次最多操作 0 项，当前 0 项" {
		t.Fatalf("模板归一失败: %q", got)
	}
}

func TestFacingNoticeAllowsControlledShapes(t *testing.T) {
	candidates := []string{
		NoticeTemplate("已删除 %d 个块。"),
		"该属性组标识在本工程已被占用",
	}
	cases := []struct {
		raw  string
		want bool
	}{
		{"已删除 3 个块。", true},               // 数字归一命中
		{"已删除 128 个块。", true},             // 多位计数
		{"该属性组标识在本工程已被占用", true},          // 逐字命中
		{"该属性组标识在本工程已被占用：请换一个", true},     // 形态 3：文案 + 定位信息
		{"已删除 3 个块。：补充", true},            // 形态 3 + 数字归一
		{"root:x:0:0:/etc/passwd", false}, // 手拼文案
		{"系统内部错误，请稍后重试（伪造的）", false},      // 近似但不等于候选
		{"已删除 3 个块", false},               // 少一个句号
		{"已删除 3 个块。额外塞进来的字", false},       // 候选后面还有别的文字（非分隔符）
		{"", false}, // 空串
	}
	for _, tc := range cases {
		got := FacingNotice(tc.raw, candidates)
		if tc.want && got == "" {
			t.Fatalf("FacingNotice(%q) 应命中", tc.raw)
		}
		if !tc.want && got != "" {
			t.Fatalf("FacingNotice(%q) 不应命中，got %q", tc.raw, got)
		}
	}
}

func TestFacingNoticeRejectsOversized(t *testing.T) {
	candidates := []string{"已删除 0 个块。"}
	long := "已删除 " + strings.Repeat("9", NoticeMaxBytes) + " 个块。"
	if got := FacingNotice(long, candidates); got != "" {
		t.Fatalf("超过 %d 字节的回执必须判未命中，got %q", NoticeMaxBytes, got)
	}
}

func TestFacingNoticeMatchesBulkIDsNotice(t *testing.T) {
	c := noticeCtx(t, "/admin/blocks")
	err := &BulkIDsError{Count: 500, Max: MaxBulkIDs}
	// 写侧经 BulkIDsFacingText 生成的文案必须能被读侧认出来 ——
	// 认不出来时运营看到的是「系统内部错误」，而不是「请分批进行」。
	text := BulkIDsFacingText(c, err)
	if got := FacingNotice(text, []string{BulkIDsNoticeTemplate(c)}); got != text {
		t.Fatalf("批量上限提示应被读侧白名单认出，got %q（文案 %q）", got, text)
	}
}

// TestLangRedirectPathIsTheOnlyCriterion LangRedirectPath 是语言回跳的**唯一一份判据**。
//
// 它被两侧共用：渲染侧（LangRedirect，写语言切换表单的隐藏域）与消费侧
// （admin 的 adminSafeLangRedirect，校验 query 带回来的值）。此前消费侧重抄了一遍判据，
// 靠一条对照测试防漂移；现在判据只有这一处实现，这条用例把四条判据逐项钉住 ——
// 放宽任何一条都是开放重定向面（//evil.example.com 会被浏览器当成协议相对 URL）。
//
// 返回空串表示拒绝：回落语义不在这一层（渲染侧回落当前请求路径、消费侧回落 "/"）。
func TestLangRedirectPathIsTheOnlyCriterion(t *testing.T) {
	long := "/admin/products?project=" + strings.Repeat("x", 300)
	over := "/admin/products?project=" + strings.Repeat("x", 520)
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"首页", "/", "/"},
		{"路径带 query", "/admin/roles?page=2&keyword=x", "/admin/roles?page=2&keyword=x"},
		{"512 字节以内原样", long, long},
		{"超过 512 字节拒绝", over, ""},
		{"超长路径拒绝", "/admin/" + strings.Repeat("y", 600), ""},
		{"空串拒绝", "", ""},
		{"不以 / 开头拒绝", "admin/system/roles", ""},
		{"协议相对跳转拒绝", "//evil.example.com/x", ""},
		{"绝对 URL 拒绝", "https://evil.example.com/x", ""},
		{"反斜杠拒绝", "/admin/roles\\x", ""},
		{"换行拒绝", "/admin/roles\nSet-Cookie: a=b", ""},
		{"NUL 拒绝", "/admin/roles\x00", ""},
		{"ESC 拒绝", "/admin/roles\x1b[31m", ""},
		{"DEL 拒绝", "/admin/roles\x7f", ""},
	}
	for _, tc := range cases {
		if got := LangRedirectPath(tc.raw); got != tc.want {
			t.Errorf("%s：LangRedirectPath(%q)=%q，want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestLangRedirectConverges(t *testing.T) {
	t.Run("站内相对路径原样", func(t *testing.T) {
		c := noticeCtx(t, "/admin/products?project=abc&page=2")
		if got := LangRedirect(c); got != "/admin/products?project=abc&page=2" {
			t.Fatalf("站内 URI 应原样，got %q", got)
		}
	})
	t.Run("超长 query 丢弃回落路径", func(t *testing.T) {
		c := noticeCtx(t, "/admin/products?project="+strings.Repeat("x", 600))
		if got := LangRedirect(c); got != "/admin/products" {
			t.Fatalf("超长 URI 应回落到当前路径，got %q", got)
		}
	})
	// 回退落点是**控制面首页** /admin，不是 "/"：站点独占域名根之后 "/" 是前台首页，
	// 后台一次输入不合法把用户甩到前台站点上，比原地不动更糟（也丢掉了登录态语境）。
	t.Run("超长路径回落控制面首页", func(t *testing.T) {
		c := noticeCtx(t, "/admin/"+strings.Repeat("y", 600))
		if got := LangRedirect(c); got != "/admin" {
			t.Fatalf("超长路径应回控制面首页，got %q", got)
		}
	})
	t.Run("协议相对路径被拒", func(t *testing.T) {
		c := noticeCtx(t, "/admin/products")
		c.Request.URL.Path = "//evil.example.com/x"
		if got := LangRedirect(c); got != "/admin" {
			t.Fatalf("非站内路径应回控制面首页，got %q", got)
		}
	})
}

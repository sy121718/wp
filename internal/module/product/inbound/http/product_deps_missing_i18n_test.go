package producthttp

// product_deps_missing_i18n_test.go — 详情页模板页「依赖未装配」这条提示的写侧 ⇄ 读侧对账。
//
// 背景：五个写入口（新建 / 首次发布 / 改 URL / 切换模板 / 预览）在契约未装配时都要回
// 详情页并带 ?err=，其中预览入口原先是一行 `c.String(400, "%s", errTemplateDepsMissing)`
// 的纯文本（脱壳、不翻译、新标签页里什么都没有）。本批统一成 303 + ?err=，文案接 i18n。
//
// 本文件守的正是「两边不同步就静默」的那一层：
//   · 写侧取词的产物（key / 中文兜底 / 当前语言译文）必须在读侧候选里，漏一种就表现为
//     「写侧发了提示、页面上什么都不显示」（不报错、不记日志）；
//   · 出口形态必须是可回来源页的 303 + QueryEscape 过的 ?err=，否则整条提示会被
//     文案里的 `&` / `#` 截断，或者用户压根没有页面可退。
//
// 与 product_notice_test.go 的分工：那里守「通用形态 + 两张白名单 + 批量模板」，
// 这里只守本批新接 i18n 的这条文案。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestProductOwnPageTextsCoverDepsMissingForms 读侧候选必须覆盖写侧三种取值形态。
func TestProductOwnPageTextsCoverDepsMissingForms(t *testing.T) {
	c := productErrCtx(t, "")
	registered := map[string]bool{}
	for _, msg := range productOwnPageTexts(c) {
		registered[msg] = true
	}
	// key 与中文兜底：i18n 未初始化时写侧产出的是兜底，初始化后取到的是译文 ——
	// 两种环境都要能对上，所以这两条必须各自在候选里。
	for _, want := range []string{detailTemplateDepsMissingKey, errTemplateDepsMissing} {
		if !registered[want] {
			t.Errorf("读侧候选缺少 %q：写侧产出这一形态时页面会静默无提示", want)
		}
	}
	// 译文形态用**写侧同一个取法**取，不手抄：手抄的候选会在改词条时与写侧静默分叉。
	if got := detailTemplateDepsMissingText(c); !registered[got] {
		t.Errorf("写侧产物 %q 不在读侧候选里：页面会静默无提示", got)
	}
}

// TestProductDepsMissingRedirectIsFacing 五个写入口共用的出口：303 + 可被读侧认出的提示。
//
// 只测单侧函数看不出两边失配（写侧拼 URL、读侧白名单各测各的永远是绿的），
// 所以这里把「POST 表单 → 重定向 → 读侧出口」整条链路走一遍。
func TestProductDepsMissingRedirectIsFacing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	form := url.Values{"projectId": {"p1"}, "productId": {"prod1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/products/template/preview",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	(&productPageHandle{}).detailTemplateDepsMissingRedirect(c)
	// gin 的 responseWriter 只把状态码记在内部，真正写到底层要等 handler 返回后由引擎
	// 调 WriteHeaderNow；这里手工调用 handler，所以自己 flush 一次（否则 rec.Code 恒为 200）。
	c.Writer.WriteHeaderNow()

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("依赖未装配应 303 回来源页，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("回跳地址无法解析：%v（%s）", err, loc)
	}
	// 回跳目标必须带 project / product：裸 /admin/products/template 会落到页面 handler 的
	// 缺参分支，再重定向到商品列表并提示「请先选一件商品」——把装配缺陷说成用户没选商品。
	if parsed.Path != productDetailTemplatePath {
		t.Errorf("回跳路径 = %q，期望 %q", parsed.Path, productDetailTemplatePath)
	}
	if parsed.Query().Get("project") != "p1" || parsed.Query().Get("product") != "prod1" {
		t.Errorf("回跳应带上工程与商品，实际 %s", loc)
	}

	readCtx := productCtxWithQuery(t, parsed.RawQuery)
	want := detailTemplateDepsMissingText(readCtx)
	if want == "" {
		t.Fatal("写侧产出了空文案：页面提示不能是空白")
	}
	if got := productPageErr(readCtx); got != want {
		t.Errorf("写侧放进 ?err= 的文案读侧认不出来：got %q want %q", got, want)
	}
}

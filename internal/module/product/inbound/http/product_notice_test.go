package producthttp

// product_notice_test.go — 商品后台页 ?err= / ?done= 读侧出口的回归。
//
// 为什么必须有：详情页模板页与捆绑页此前把 `c.Query("err")` **原样**塞进模板数据
//（`"Err": strings.TrimSpace(c.Query("err"))`），手拼一个 /admin/products/template?err=任意文案
// 就能往页面上塞一条顶着「上一次操作未完成」样式的伪造消息。改成 productPageErr / productPageDone
// 之后还有第二个坑：这两页的文案**不全走 productErrText**（详情页模板页有两张自己的白名单，
// 捆绑页有参数级提示）—— 漏登记的症状不是「文案不准」，而是这条业务提示被归口文案整体顶掉
//（运营看到「系统内部错误」，实际原因只是「模板名不能为空」）。
//
// 所以断言分两层：受控形态能进出、写侧真的会产出的每一种取值都能被认出来。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	presentationenums "go_wp/internal/module/presentation/enums"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"
)

func productCtxWithQuery(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	target := "/admin/products/template"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}

func productErrCtx(t *testing.T, raw string) *gin.Context {
	t.Helper()
	q := url.Values{}
	if strings.TrimSpace(raw) != "" {
		q.Set("err", raw)
	}
	return productCtxWithQuery(t, q.Encode())
}

func productDoneCtx(t *testing.T, raw string) *gin.Context {
	t.Helper()
	q := url.Values{}
	if strings.TrimSpace(raw) != "" {
		q.Set("done", raw)
	}
	return productCtxWithQuery(t, q.Encode())
}

// TestProductPageErrAcceptsBothEnumsForms 商品域 enums 的两种形态（常量名 / 当前语言文案）都认。
//
// 写侧 productErrText 出的是译文，而 i18n 未初始化或该 key 没有词条时兜底是 productErrFallbacks
// 的中文；两条路都要能对上，否则「文案备好了却永远不出现」。
func TestProductPageErrAcceptsBothEnumsForms(t *testing.T) {
	for _, key := range []string{productenums.ErrNotFound, productenums.ErrSlugTaken, productenums.ErrBundlePriceRequired} {
		fallback := productErrFallbacks[key]
		if fallback == "" {
			t.Fatalf("测试素材缺失：%s 没有中文兜底", key)
		}
		if got := productPageErr(productErrCtx(t, key)); got != key {
			t.Errorf("常量名形态应被放行，实际 %q（key=%s）", got, key)
		}
		if got := productPageErr(productErrCtx(t, fallback)); got != fallback {
			t.Errorf("译文 / 兜底形态应被放行，实际 %q（fallback=%s）", got, fallback)
		}
	}
}

// TestProductPageErrAcceptsPageOwnTexts 详情页模板页与捆绑页的自有文案必须都在候选里。
//
// 这是本批最容易漏的一层：这两页的文案不由 productErrText 产出，读侧若不登记，
// 业务提示会被 shell.PageInternalText 顶掉（回落到归口文案时还会**记一条日志**，
// 看起来一切正常，只有运营发现「说什么都是系统内部错误」）。
func TestProductPageErrAcceptsPageOwnTexts(t *testing.T) {
	owner := []string{
		errTemplateDepsMissing,
		productDetailTemplateNameRequired,
		productDetailTemplatePathRequired,
		productBundleNoProductText,
	}
	for _, msg := range detailTemplateFacingMessages {
		owner = append(owner, msg)
	}
	for _, msg := range detailTemplateTemplateMessages {
		owner = append(owner, msg)
	}
	for _, msg := range owner {
		if got := productPageErr(productErrCtx(t, msg)); got != msg {
			t.Errorf("本页自造文案应原样放行，实际 %q（want %q）", got, msg)
		}
	}
	// 两张白名单的键各取一个，钉住「查表拿到的是中文」这条链路。
	for _, msg := range []string{
		detailTemplateFacingMessages[presentationenums.ErrPathOccupied],
		detailTemplateTemplateMessages[contenttemplateenums.ErrNotFound],
	} {
		if msg == "" {
			t.Fatal("测试素材缺失：白名单里应登记该 key")
		}
		if got := productPageErr(productErrCtx(t, msg)); got != msg {
			t.Errorf("白名单值应原样放行，实际 %q（want %q）", got, msg)
		}
	}
}

// TestProductPageErrFallsBackForForged 未命中落归口文案（错误提示必须说点什么）。
func TestProductPageErrFallsBackForForged(t *testing.T) {
	for _, raw := range []string{
		"系统维护中，请稍后重试",
		"<script>alert(1)</script>",
		"<script>alert(1)</script>" + productErrFallbacks[productenums.ErrNotFound],
		productErrFallbacks[productenums.ErrNotFound] + "<script>alert(1)</script>",
		"http://169.254.169.254/latest/meta-data/",
	} {
		c := productErrCtx(t, raw)
		got := productPageErr(c)
		if got != shell.PageInternalText(c) {
			t.Errorf("未命中应落归口文案，实际 %q（raw=%q）", got, raw)
		}
	}
	if got := productPageErr(productErrCtx(t, "")); got != "" {
		t.Errorf("没有 err 参数时不该凭空出一条错误提示，实际 %q", got)
	}
}

// TestProductPageDoneMissesToEmpty 成功态的 fallback 是空串（不是归口文案）。
func TestProductPageDoneMissesToEmpty(t *testing.T) {
	if got := productPageDone(productDoneCtx(t, "已删除 3 个标签")); got != "已删除 3 个标签" {
		t.Errorf("写侧批量结论应被放行，实际 %q", got)
	}
	for _, raw := range []string{"", "伪造的成功文案", strings.Repeat("已删除 3 个标签", 100)} {
		if got := productPageDone(productDoneCtx(t, raw)); got != "" {
			t.Errorf("未命中应落空串，实际 %q（raw=%q）", got, raw)
		}
	}
}

// TestProductDetailTemplateBackURLRoundTrip 详情页模板页的写侧回跳 ⇄ 读侧出口整体对账。
//
// 写侧四种失败文案（模板契约未装配 / 参数级提示 / 两张白名单）都要能活着回到页面上，
// 这条用例把「写侧拼 URL → 读侧认出来」整条链路走一遍 —— 只测单侧函数看不出两边失配。
func TestProductDetailTemplateBackURLRoundTrip(t *testing.T) {
	h := &productPageHandle{}
	writers := []string{
		errTemplateDepsMissing,
		productDetailTemplateNameRequired,
		productDetailTemplatePathRequired,
		detailTemplateFacingMessages[presentationenums.ErrInvalidPath],
		detailTemplateTemplateMessages[contenttemplateenums.ErrDataInvalid],
	}
	for _, msg := range writers {
		loc := h.detailTemplateBackURL("p1", "prod1", msg)
		parsed, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("回跳地址无法解析：%v", err)
		}
		if got := productPageErr(productCtxWithQuery(t, parsed.RawQuery)); got != msg {
			t.Errorf("写侧放进 ?err= 的文案读侧认不出来：got %q want %q", got, msg)
		}
	}
	// 成功回跳不带 err：页面不该出现任何提示。
	loc := h.detailTemplateBackURL("p1", "prod1", "")
	parsed, _ := url.Parse(loc)
	if got := productPageErr(productCtxWithQuery(t, parsed.RawQuery)); got != "" {
		t.Errorf("成功回跳不该带错误提示，实际 %q", got)
	}
}

// TestProductBulkTemplatesAreAllRecognised 每一条批量结论模板都要能被读侧认出来。
//
// 这条用例是为了钉住一个真实踩到的坑：候选曾经写成 `Sprintf(tpl, 0, 0)`，
// 单 %d 的模板会因此多出 `%!(EXTRA int=0)` —— 五条「已删除 N 个标签 / 属性组 / 分类 /
// 品牌 / 商品」的批量回执于是**永远**匹配不上，页面上的成功提示静默消失。
// 按占位符个数生成实例（写侧就是这么 Sprintf 的），逐个走一遍判定。
func TestProductBulkTemplatesAreAllRecognised(t *testing.T) {
	for _, tpl := range productBulkResultTemplates {
		args := make([]any, strings.Count(tpl, "%d"))
		for i := range args {
			args[i] = 3
		}
		rendered := fmt.Sprintf(tpl, args...)
		if got := productPageDone(productDoneCtx(t, rendered)); got != rendered {
			t.Errorf("模板 %q 的实例读侧认不出来（got %q）—— 写侧放进去的回执会在页面上消失", tpl, got)
		}
	}
}

package producthttp

// product_deps_missing_i18n_test.go — 详情页模板页「依赖未装配」这条提示的出口形态。
//
// 背景：五个写入口（新建 / 首次发布 / 改 URL / 切换模板 / 预览）在契约未装配时都要回
// 详情页并给一句可读提示，其中预览入口原先是一行 `c.String(400, "%s", errTemplateDepsMissing)`
// 的纯文本（脱壳、不翻译、新标签页里什么都没有）。本批统一成**整页提示**
//（shell.RenderJump，文案走响应体、不进 URL），取代早先的 303 + ?err=。
//
// 本文件守两件错了会静默的事：
//   · 提示文案取词必须非空（取词失败会让提示页显示一句空话，用户以为操作成功了）；
//   · 出口形态必须是可回来源页的整页提示（HTTP 200 + data-jump-state="err" + 受控文案 +
//     带回工程与商品的回跳链接），否则用户既不知道缺什么、也没有回去的地方。

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestProductDepsMissingTextNonEmpty 写侧取词必须给出非空文案。
func TestProductDepsMissingTextNonEmpty(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "", "")
	if got := strings.TrimSpace(detailTemplateDepsMissingText(c)); got == "" {
		t.Fatal("写侧产出了空文案：页面提示不能是空白")
	}
}

// TestProductDepsMissingRedirectIsFacing 五个写入口共用的出口：整页提示回详情页模板页。
//
// 只测单侧函数看不出两边失配，所以这里把「POST 表单 → 提示页」整条链路走一遍。
func TestProductDepsMissingRedirectIsFacing(t *testing.T) {
	form := url.Values{"projectId": {"p1"}, "productId": {"prod1"}}
	c, rec := newProductJumpContext(t, "", "", form.Encode())
	(&productPageHandle{}).detailTemplateDepsMissingRedirect(c)
	c.Writer.WriteHeaderNow()

	if rec.Code != http.StatusOK {
		t.Fatalf("依赖未装配应渲染提示页（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("应渲染 err 态提示页，body=%s", body)
	}
	if want := detailTemplateDepsMissingText(c); !strings.Contains(body, want) {
		t.Fatalf("提示页应含 %q，body=%s", want, body)
	}
	// 回跳目标必须带 project / product：裸 /admin/products/template 会落到页面 handler 的
	// 缺参分支，再提示「请先选一件商品」——把装配缺陷说成用户没选商品。
	if !strings.Contains(body, "/admin/products/template?") {
		t.Fatalf("回跳应回详情页模板页，body=%s", body)
	}
	if !strings.Contains(body, "project=p1") || !strings.Contains(body, "product=prod1") {
		t.Fatalf("回跳应带上工程与商品，body=%s", body)
	}
	// 提示页不该带任何内部细节。
	for _, tok := range []string{"SQLSTATE", "relation \"", "does not exist"} {
		if strings.Contains(body, tok) {
			t.Fatalf("提示页泄漏内部细节 %q", tok)
		}
	}
}

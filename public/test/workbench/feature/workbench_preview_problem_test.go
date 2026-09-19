package feature

// workbench_preview_problem_test.go — 结构模板预览 422 的**双向**回归（两条都要有，缺一条等于没验证）：
//
//	① 作者可操作的组件校验提示必须原样出现在响应体里 —— 这是工作台画布的核心价值：
//	   作者在画布上直接看到「哪里坏了、怎么修」。判据是 page/service 打的
//	   pagecontract.PreviewProblem 类型标记（结构化），不是嗅探错误文本；
//	② 内部错误（装配缺失 / 模板加载失败）**不得**把原文透出去，只给归口文案。
//
// 为什么必须走真实 HTTP + 真实 page 编译管线：这两条断言考的正是
// 「错误在产生处被标记 → 包装链没把类型丢掉 → 消费侧 errors.As 命中」这条链，只测消费侧（workbench 包内的合成错误）覆盖不到中间那两段。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

// workbenchEmptyAccordionDoc 含**校验问题**的文档：空手风琴（组件要求至少一个折叠项，
// 校验器返回裸 fmt.Errorf —— 正是过去被压成一句泛化文案的那类）。
const workbenchEmptyAccordionDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.accordion","id":"acc1","props":{}}]}`

// workbenchNavWithoutResolverDoc 含**内部错误**的文档：导航节点绑定了菜单位置，
// 而预览环境没有注入导航解析器（ctx.Navigation == nil）—— jetview 显式报「装配未注入」，
// 这是构建装配问题，不是作者能在画布上修的东西。
const workbenchNavWithoutResolverDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.nav","id":"nav1","props":{"menu":"header"}}]}`

// postStructureTemplateDraft 以未保存草稿请求结构模板预览（与画布同一入口）。
func postStructureTemplateDraft(t *testing.T, env *workbenchTemplateEnv, templateID, projectID, doc string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{
		"id":            {templateID},
		"entityType":    {"header"},
		"projectId":     {projectID},
		"draftDocument": {doc},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/template/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	env.engine.ServeHTTP(rec, req)
	return rec
}

// TestStructureTemplatePreviewShowsValidationProblem ① 组件校验提示带原文透出（不是泛化文案）。
func TestStructureTemplatePreviewShowsValidationProblem(t *testing.T) {
	env := newWorkbenchTemplateEnv(t)
	if env == nil {
		return
	}
	headerID := env.create(t, "站点页眉（含校验问题）", contenttemplatemodel.EntityTypeHeader, workbenchStructureDoc)

	rec := postStructureTemplateDraft(t, env, headerID, env.projectID, workbenchEmptyAccordionDoc)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("含校验问题的草稿应 422，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "手风琴至少需要一个折叠项") {
		t.Fatalf("响应体应透出组件校验提示原文（作者可操作），实际：%s", firstN(body, 400))
	}
	if !strings.Contains(body, "acc1") {
		t.Fatalf("响应体应带节点定位（作者据此在画布上找到组件），实际：%s", firstN(body, 400))
	}
}

// TestStructureTemplatePreviewInternalErrorFallsBackToFacingText ② 内部错误只给归口文案。
func TestStructureTemplatePreviewInternalErrorFallsBackToFacingText(t *testing.T) {
	env := newWorkbenchTemplateEnv(t)
	if env == nil {
		return
	}
	headerID := env.create(t, "站点页眉（装配缺失）", contenttemplatemodel.EntityTypeHeader, workbenchStructureDoc)

	rec := postStructureTemplateDraft(t, env, headerID, env.projectID, workbenchNavWithoutResolverDoc)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("装配缺失的草稿应 422，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	for _, leak := range []string{"装配未注入", "导航解析器", "nav1", "页面编译失败"} {
		if strings.Contains(body, leak) {
			t.Fatalf("内部错误原文不得进响应（%q 命中）：%s", leak, firstN(body, 400))
		}
	}
	if !strings.Contains(body, "预览编译失败") {
		t.Fatalf("内部错误应给归口文案，实际：%s", firstN(body, 400))
	}
}

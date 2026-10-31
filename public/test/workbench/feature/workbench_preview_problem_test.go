package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

const workbenchEmptyAccordionDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.accordion","id":"acc1","props":{}}]}`

const workbenchNavWithoutResolverDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.nav","id":"nav1","props":{"menu":"header"}}]}`

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

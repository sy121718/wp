package feature

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"
)

// ===== 模型目录保存的 JSON 输入形状 =====

// saveModelsJSON 按浏览器真实提交形态打 /api/ai/provider/models/save：
// 明确的 JSON 对象（providerId / version / models[]），由 ai-models.js 的 payload builder
// 组装、HTMX 发送。目录行是结构化对象，不再用 multipart 把 JSON 串藏在普通字段里。
func saveModelsJSON(t *testing.T, h *aihttp.Handle, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/ai/provider/models/save", pageview.HTMLFragment, h.SaveModels)

	body := "{" + strings.Join([]string{
		`"providerId":` + fields["providerId"],
		`"version":` + fields["version"],
		`"models":` + fields["models"],
	}, ",") + "}"
	req := httptest.NewRequest(http.MethodPost, "/api/ai/provider/models/save", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// 真实的那一次提交：成功回 Jet 片段，目录按提交内容落库。
func TestAIModelsSaveAcceptsJSONArray(t *testing.T) {
	h, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "multipart-gateway",
		DisplayName: "Multipart Gateway",
	})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}

	// 单模型、显示名留空、inputTypes 只给 image：三条归一事实一次覆盖。
	rec := saveModelsJSON(t, h, map[string]string{
		"providerId": strconv.FormatInt(p.ID, 10),
		"version":    strconv.FormatInt(p.Version, 10),
		"models":     `[{"id":"gpt-4o","displayName":"","contextWindow":128000,"maxOutputTokens":4096,"inputTypes":["image"]}]`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("保存应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	if !strings.Contains(rec.Body.String(), "gpt-4o") {
		t.Fatalf("保存片段应回新的模型目录：%s", rec.Body.String())
	}

	got, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读供应商失败：%v", err)
	}
	if len(got.Models) != 1 {
		t.Fatalf("目录应有 1 个模型，实际 %d", len(got.Models))
	}
	m := got.Models[0]
	if m.ID != "gpt-4o" {
		t.Errorf("模型 ID 应为 gpt-4o，实际 %q", m.ID)
	}
	// 空显示名回退 ID。
	if m.DisplayName != "gpt-4o" {
		t.Errorf("空显示名应回退模型 ID，实际 %q", m.DisplayName)
	}
	// inputTypes 归一：只勾 image 时要给出稳定的集合。
	if len(m.InputTypes) != 1 || m.InputTypes[0] != aienums.InputTypeImage {
		t.Errorf("inputTypes 应归一为 [image]，实际 %v", m.InputTypes)
	}
	if m.ContextWindow != 128000 {
		t.Errorf("contextWindow 应保留 128000，实际 %d", m.ContextWindow)
	}
	if m.MaxOutputTokens != 4096 {
		t.Errorf("maxOutputTokens 应保留 4096，实际 %d", m.MaxOutputTokens)
	}
	// 版本必须真的往上走：保存成功却不带版本回执，前端下一次保存就会 409。
	if got.Version <= p.Version {
		t.Errorf("保存后版本应递增，实际 %d → %d", p.Version, got.Version)
	}
}

// inputTypes 全空时归一成 text：这是 normalizeInputTypes 的既有语义，也是一条正常提交。
func TestAIModelsSaveNormalizesEmptyInputTypesToText(t *testing.T) {
	h, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "empty-types-gateway",
		DisplayName: "Empty Types",
	})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}

	rec := saveModelsJSON(t, h, map[string]string{
		"providerId": strconv.FormatInt(p.ID, 10),
		"version":    strconv.FormatInt(p.Version, 10),
		"models":     `[{"id":"plain-model","inputTypes":[]}]`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	got, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if len(got.Models) != 1 || len(got.Models[0].InputTypes) != 1 || got.Models[0].InputTypes[0] != aienums.InputTypeText {
		t.Fatalf("空 inputTypes 应归一为 [text]，实际 %+v", got.Models)
	}
}

// 非法 JSON 是 400 参数错误，且目录一个字节都不动 —— 不能把非法输入当空目录存下去。
func TestAIModelsSaveRejectsMalformedJSON(t *testing.T) {
	h, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "bad-json-gateway",
		DisplayName: "Bad JSON",
	})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}
	// 先存一条，用来证明失败提交没有清空目录。
	if _, err = svc.SaveModels(ctx, &aidto.SaveModelsReq{
		ProviderID: p.ID,
		Version:    p.Version,
		Models:     []aidto.ModelEntry{{ID: "keep-me"}},
	}); err != nil {
		t.Fatalf("预处理保存失败：%v", err)
	}
	before, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}

	rec := saveModelsJSON(t, h, map[string]string{
		"providerId": strconv.FormatInt(p.ID, 10),
		"version":    strconv.FormatInt(before.Version, 10),
		"models":     `[{"id":"broken"`,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应为 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
	}

	after, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if len(after.Models) != 1 || after.Models[0].ID != "keep-me" {
		t.Fatalf("非法提交不应改动目录，实际 %+v", after.Models)
	}
	if after.Version != before.Version {
		t.Errorf("非法提交不应推版本，实际 %d → %d", before.Version, after.Version)
	}
}

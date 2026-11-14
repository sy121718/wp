package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	json "go_wp/pkg/jsonx"
)

// 编辑器的 JSON 经真实处理器和服务落库，query 不得补齐或覆盖写 body。
func TestContentTemplateEditorJSONPersistsDocument(t *testing.T) {
	env := newContentTemplateAdminEnv(t)
	if env == nil {
		return
	}
	handle := contenttemplatehttp.NewHandle(env.templates)
	env.engine.POST("/api/contenttemplate/create", handle.Create)
	env.engine.POST("/api/contenttemplate/update", handle.Update)
	post := func(path string, input any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		env.engine.ServeHTTP(rec, req)
		assertNoContentTemplateInternalLeak(t, "编辑器 JSON", rec.Body.String())
		return rec
	}
	created := post("/api/contenttemplate/create?name=query&projectId=query", &contenttemplatedto.CreateReq{
		ProjectID: env.projectID, EntityType: "header", Name: "编辑器模板",
		DraftDocument: json.RawMessage(contentTemplateAdminDoc),
	})
	var result struct {
		Data contenttemplatedto.TemplateResp `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || created.Code != http.StatusOK || result.Data.ID == "" {
		t.Fatalf("JSON 创建必须成功：status=%d body=%s err=%v", created.Code, created.Body.String(), err)
	}
	id := result.Data.ID
	document := strings.ReplaceAll(contentTemplateAdminDoc, "结构", "保存后的结构")
	updated := post("/api/contenttemplate/update?id=query", &contenttemplatedto.UpdateReq{
		ID: id, DraftDocument: json.RawMessage(document),
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("JSON 更新必须成功：status=%d body=%s", updated.Code, updated.Body.String())
	}
	stored, err := env.templates.GetScoped(t.Context(), env.projectID, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "编辑器模板" || !strings.Contains(string(stored.DraftDocument), "保存后的结构") {
		t.Fatalf("落库必须来自 body 的工程、名称和文档：%+v", stored)
	}

	rejected := post("/api/contenttemplate/update?id="+id, map[string]any{"draftDocument": json.RawMessage(contentTemplateAdminDoc)})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("query 不能补必填 id：status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	after, err := env.templates.Get(t.Context(), &contenttemplatedto.GetReq{ID: id})
	if err != nil || string(after.DraftDocument) != string(stored.DraftDocument) {
		t.Fatalf("非法更新不得改变已存文档：after=%+v err=%v", after, err)
	}
}

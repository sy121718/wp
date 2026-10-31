package feature

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
)

func TestProductBulkHTTPContract(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}

	engine.POST("/api/product/bulk-delete", producthttp.NewProductHandle(f.svc).BulkDelete)

	t.Run("超过上限：400 且文案带两个数字", func(t *testing.T) {
		ids := make([]string, 201)
		for i := range ids {
			ids[i] = uuid.NewString()
		}
		rec := postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
			"projectId": f.projectID,
			"ids":       ids,
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("超限应 400，实际 %d：%s", rec.Code, rec.Body.String())
		}
		env := decodeJSONEnvelope(t, rec)

		if !strings.Contains(env.Message, "200") || !strings.Contains(env.Message, "201") {
			t.Fatalf("文案应带上限与本次条数，实际 %q", env.Message)
		}
	})

	t.Run("没勾选：400 而不是删除 0 个的成功", func(t *testing.T) {
		rec := postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
			"projectId": f.projectID,
			"ids":       []string{},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("空勾选应 400，实际 %d：%s", rec.Code, rec.Body.String())
		}
		if msg := decodeJSONEnvelope(t, rec).Message; strings.TrimSpace(msg) == "" {
			t.Fatal("空勾选应给一句可读文案")
		}
	})

	t.Run("夹着不存在的 id：409 + data.items 逐条定位", func(t *testing.T) {
		price := 12.0
		p, err := f.svc.Create(t.Context(), &productdto.CreateReq{
			ProjectID: f.projectID, Name: "批量契约商品", Slug: "bulk-http-contract", DefaultPrice: &price,
		})
		if err != nil {
			t.Fatalf("建商品失败: %v", err)
		}
		rec := postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
			"projectId": f.projectID,
			"ids":       []string{p.ID, "11111111-1111-4111-8111-111111111111"},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("整批被拒应 409，实际 %d：%s", rec.Code, rec.Body.String())
		}
		env := decodeJSONEnvelope(t, rec)
		items, _ := env.Data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("data.items 应只有 1 条（不存在的那个），实际 %v", items)
		}
		row, _ := items[0].(map[string]any)
		if id, _ := row["id"].(string); id != "11111111-1111-4111-8111-111111111111" {
			t.Fatalf("条目应指向不存在的 id，实际 %v", row["id"])
		}
		if key, _ := row["reasonKey"].(string); key != "ErrNotFound" {
			t.Fatalf("reasonKey 应为 ErrNotFound，实际 %v", row["reasonKey"])
		}
		msg, _ := row["message"].(string)
		if strings.TrimSpace(msg) == "" || strings.Contains(msg, "ErrNotFound") {
			t.Fatalf("message 应是当前语言的文案而不是裸 key，实际 %q", msg)
		}

		if _, gerr := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: p.ID}); gerr != nil {
			t.Fatalf("整批被拒时商品不该被删：%v", gerr)
		}
	})
}

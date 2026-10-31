package feature

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	bulkids "go_wp/pkg/bulkids"
)

var productInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoProductInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range productInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

func newProductErrorLeakEnv(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := producthttp.NewProductHandle(f.svc)
	engine.POST("/api/product/delete", handle.Delete)
	engine.POST("/api/product/bulk-delete", handle.BulkDelete)
	return engine, f
}

func TestProductPageHidesInternalError(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	const pid = "11111111-1111-1111-1111-111111111111"

	if err := f.db.Exec("ALTER TABLE products RENAME TO products_hidden").Error; err != nil {
		t.Fatalf("改名 products 表失败：%v", err)
	}

	rawErr := f.svc.Delete(context.Background(), &productdto.DeleteReq{ID: pid})
	if rawErr == nil {
		t.Fatalf("products 表不存在时删除应失败")
	}
	if !strings.Contains(rawErr.Error(), "products") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误不含表名 / SQLSTATE：%v", rawErr)
	}

	env := decodeJSONEnvelope(t, postJSON(t, engine, "/api/product/delete", map[string]any{
		"projectId": f.projectID, "id": pid,
	}))
	if env.Code == http.StatusOK {
		t.Fatalf("内部错误不该回成功")
	}
	assertNoProductInternalLeak(t, "JSON 回执", env.Message)

	if !strings.Contains(env.Message, "服务器内部错误") {
		t.Fatalf("内部错误应给归口文案，message=%s", env.Message)
	}
}

func TestProductPageKeepsBusinessErrorText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}

	env := decodeJSONEnvelope(t, postJSON(t, engine, "/api/product/delete", map[string]any{
		"projectId": f.projectID, "id": "11111111-1111-1111-1111-111111111111",
	}))
	if env.Code == http.StatusOK {
		t.Fatalf("删不存在的商品应失败")
	}
	assertNoProductInternalLeak(t, "JSON 回执", env.Message)

	if env.Message != productenums.FallbackFor(productenums.ErrNotFound) {
		t.Fatalf("业务错误应透出声明处的兜底文案，message=%s", env.Message)
	}
}

func TestProductPageKeepsBulkLimitText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	env := decodeJSONEnvelope(t, postJSON(t, engine, "/api/product/bulk-delete", map[string]any{
		"projectId": f.projectID, "ids": ids,
	}))
	if env.Code == http.StatusOK {
		t.Fatalf("超限应整批拒绝，实际成功")
	}
	assertNoProductInternalLeak(t, "JSON 回执", env.Message)

	if !strings.Contains(env.Message, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，message=%s", env.Message)
	}
	if !strings.Contains(env.Message, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(env.Message, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控提示应带上限与本次条数，message=%s", env.Message)
	}
}

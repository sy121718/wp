package feature

// 商品写接口的 JSON 错误不暴露数据库细节，业务错误保留声明处文案。
// 通过真实数据库故障验证统一响应出口，而不是用伪造错误代替链路。

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

// productInternalLeakTokens 内部细节指纹：出现任一即视为泄漏。
//
// 取的是「PG 原文与库结构里一定出现、而业务文案里一定不出现」的串：
// 42P01 的原文形如 relation "products" does not exist (SQLSTATE 42P01)。
var productInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

// assertNoProductInternalLeak 断言文本不含任何内部细节指纹。
func assertNoProductInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range productInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// newProductErrorLeakEnv 装配只挂两个写端点的测试引擎（真实 service + 真实 PG）。
// 商品保存已迁到 JSON 写基座直调 /api/product/update，页面那条 POST 不存在了；
// 删除 / 批量删除同理，唯一出口是 JSON 回执 —— 判据也跟着搬到 message 上。
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

// TestProductPageHidesInternalError 真实基础设施错误（products 表不存在）只进日志：
// JSON message 必须是归口文案，不含表名 / SQLSTATE / 约束名。
func TestProductPageHidesInternalError(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	const pid = "11111111-1111-1111-1111-111111111111"

	// 把依赖「改名」制造真实故障（而不是伪造错误字符串）。
	if err := f.db.Exec("ALTER TABLE products RENAME TO products_hidden").Error; err != nil {
		t.Fatalf("改名 products 表失败：%v", err)
	}
	// 反证：service 层的原始错误确实带表名与 SQLSTATE —— 否则这个用例什么也证明不了。
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
	// 未装配 i18n 时，统一 JSON 出口仍须给内部错误的安全兜底。
	if !strings.Contains(env.Message, "服务器内部错误") {
		t.Fatalf("内部错误应给归口文案，message=%s", env.Message)
	}
}

// TestProductPageKeepsBusinessErrorText 业务文案采用错误声明处的兜底。
func TestProductPageKeepsBusinessErrorText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	// 删不存在的商品返回 ErrNotFound 的声明文案「商品或变体不存在」。
	env := decodeJSONEnvelope(t, postJSON(t, engine, "/api/product/delete", map[string]any{
		"projectId": f.projectID, "id": "11111111-1111-1111-1111-111111111111",
	}))
	if env.Code == http.StatusOK {
		t.Fatalf("删不存在的商品应失败")
	}
	assertNoProductInternalLeak(t, "JSON 回执", env.Message)
	// 这个 feature 包从不装配 i18n，pkg/response 的 translate 未命中时退回**声明处的兜底**
	// （productenums.Err 用 FallbackFor 给错误配的中文）—— 所以在这个引擎上
	// 「业务文案」的可见形态就是那句中文，而不是裸 key。生产装配后它被译成对应语言的词条。
	// 业务错误保留声明处事实，JSON 回执不得泄漏内部标识。
	if env.Message != productenums.FallbackFor(productenums.ErrNotFound) {
		t.Fatalf("业务错误应透出声明处的兜底文案，message=%s", env.Message)
	}
}

// TestProductPageKeepsBulkLimitText 批量上限拒绝的数字事实保持可见：
// 它不是 enums key，但整句由本仓库拼出、带着可行动的数字，收口后不能变成通用文案。
func TestProductPageKeepsBulkLimitText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	// 用高于上限一条即触发整批拒绝（不依赖 @10 这类经验数字）。
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
	// 受控提示必须带两个数字：只说「请求参数无效」时操作者不知道该拆成几批。
	if !strings.Contains(env.Message, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，message=%s", env.Message)
	}
	if !strings.Contains(env.Message, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(env.Message, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控提示应带上限与本次条数，message=%s", env.Message)
	}
}

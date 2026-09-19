package feature

// inventory_source_bulk_limit_text_test.go — 货源页批量删除的 id 超限路径（本批受控类型收口）。
//
// 背景：这一处此前把 berr.Error() 直接拼进 ?err=，靠门禁豁免 + 注释放行；本批把 shell.BulkIDs
// 的超限错误做成带 sentinel 的类型（shell.ErrBulkIDsTooMany / *shell.BulkIDsError），
// 页面改走 shell.BulkIDsFacingText，脚本里的豁免条目随之删除。
//
// 用 nil service：超限在读到 service 之前就被 shell.BulkIDs 拒绝，这条路径不碰数据库。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	inventoryhttp "go_wp/internal/module/product/inventory/inbound/http"
	"go_wp/internal/web/shell"
)

// sourceBulkOverLimitForm 造一份超过 shell.MaxBulkIDs 的批量表单。
func sourceBulkOverLimitForm() url.Values {
	form := url.Values{"projectId": {"00000000-0000-0000-0000-000000000000"}}
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", "00000000-0000-0000-0000-"+strconv.Itoa(2000000000000+i))
	}
	return form
}

// TestInventorySourcesBulkDeleteOverLimitUsesControlledText 超限时 302 回带受控文案，
// 两个数字（上限 / 本次条数）都在，且不含内部细节指纹。
func TestInventorySourcesBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handle := inventoryhttp.NewInventorySourcePageHandle(nil, nil)
	engine.POST("/admin/inventory/sources/bulk-delete", handle.InventorySourcesBulkDelete)

	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/admin/inventory/sources/bulk-delete",
		strings.NewReader(sourceBulkOverLimitForm().Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil || !errors.Is(rawErr, shell.ErrBulkIDsTooMany) {
		t.Fatalf("反证失败：超限应返回命中 sentinel 的错误，实际 %v", rawErr)
	}

	form := sourceBulkOverLimitForm()
	req := httptest.NewRequest(http.MethodPost, "/admin/inventory/sources/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("超限应 302 回列表页，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	rawLocation := rec.Header().Get("Location")
	loc, err := url.Parse(rawLocation)
	if err != nil {
		t.Fatalf("Location 无法解析：%v（%s）", err, rawLocation)
	}
	got := loc.Query().Get("err")
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("应回带受控提示，实际 ?err=%q", got)
	}
	over := strconv.Itoa(shell.MaxBulkIDs + 1)
	if !strings.Contains(got, strconv.Itoa(shell.MaxBulkIDs)) || !strings.Contains(got, over) {
		t.Fatalf("受控提示应带上限 %d 与本次条数 %s，实际 ?err=%q", shell.MaxBulkIDs, over, got)
	}
	for _, tok := range []string{"SQLSTATE", "constraint", "uq_", "pg_"} {
		if strings.Contains(rawLocation, tok) {
			t.Errorf("Location 泄漏内部细节 %q：%s", tok, rawLocation)
		}
	}
}

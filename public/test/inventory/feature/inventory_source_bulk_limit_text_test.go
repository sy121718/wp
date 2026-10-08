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

	inventoryhttp "go_wp/internal/module/inventory/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// sourceBulkOverLimitForm 造一份超过 shell.MaxBulkIDs 的批量表单。
func sourceBulkOverLimitForm() url.Values {
	form := url.Values{"projectId": {"00000000-0000-0000-0000-000000000000"}}
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", "00000000-0000-0000-0000-"+strconv.Itoa(2000000000000+i))
	}
	return form
}

// TestInventorySourcesBulkDeleteOverLimitUsesControlledText 超限时渲染受控提示页，
// 两个数字（上限 / 本次条数）都在，且不含内部细节指纹。
func TestInventorySourcesBulkDeleteOverLimitUsesControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(templateRoot(), true)
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
	// 超限：整批拒绝并渲染受控失败提示页（取代原先的 302 + ?err=）。
	assertInventoryJump(t, rec, "err", "一次最多操作",
		strconv.Itoa(shell.MaxBulkIDs), strconv.Itoa(shell.MaxBulkIDs+1))
	for _, tok := range []string{"SQLSTATE", "constraint", "uq_", "pg_"} {
		if strings.Contains(rec.Body.String(), tok) {
			t.Errorf("提示页泄漏内部细节 %q：%s", tok, rec.Body.String())
		}
	}
}

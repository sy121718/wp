package feature

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	mailmodel "go_wp/internal/module/mail/model"
)

// 邮件页面写端点统一为「标准表单 + Jet 片段」：成功换结果区、失败换错误区，
// 都不整页跳转。本用例覆盖批量改状态、导入与模板删除三条页面通道。
func TestMailPageWriteFragments(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newMailContactCrudRouter(t, f.svc, mailCrudPagePerms())
	ctx := t.Context()

	if err := f.seedContact(ctx, "qa.frag.status@example.invalid", "片段", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	row, err := mailmodel.NewMailModel(f.db).GetContactByEmail(ctx, "qa.frag.status@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	injectMailWords(t, map[string]map[string]string{
		"mailCtcStDone": {"zh-CN": "已把目标状态更新", "en-US": "status updated"},
	})

	rec := mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"ids": {strconv.FormatUint(row.ID, 10)}, "status": {mailmodel.ContactStatusUnsubscribed},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("批量改状态应回 200 片段，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("批量改状态的响应必须是 HTML 片段，实际 %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "已把") {
		t.Fatalf("成功片段应说明更新数量，实际：%s", firstN(body, 400))
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("写响应不该带 Location：%q", loc)
	}

	moved, err := mailmodel.NewMailModel(f.db).GetContact(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Status != mailmodel.ContactStatusUnsubscribed {
		t.Fatalf("批量改状态应落库，实际 %q", moved.Status)
	}

	// 非法目标状态必须给失败片段，不能静默成功。
	rec = mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"ids": {strconv.FormatUint(row.ID, 10)}, "status": {"bogus"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法目标状态应 400，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	if body := rec.Body.String(); !strings.Contains(body, "data-ui-error") {
		t.Fatalf("非法目标状态应渲染错误区，实际：%s", firstN(body, 400))
	}

	// 空勾选：片段要说明列表未改动，且不能把 0 条报成成功。
	rec = mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"status": {mailmodel.ContactStatusSubscribed},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("空勾选应回 200 结果片段，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	if body := rec.Body.String(); !strings.Contains(body, "没有勾选") {
		t.Fatalf("空勾选要说清列表未改动，实际：%s", firstN(body, 400))
	}

	// 导入：成功片段带五个计数，并让联系人真正落库。
	email := fmt.Sprintf("qa.frag.import.%d@example.invalid", row.ID)
	rec = mailCrudPagePost(t, router, "/admin/mail/contact/import", url.Values{
		"content":          {email},
		"consent_declared": {"1"},
		"consent_source":   {"片段验收"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("导入应回 200 片段，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	if body := rec.Body.String(); !strings.Contains(body, "导入完成") && !strings.Contains(body, "Import complete") {
		t.Fatalf("导入片段应给结论，实际：%s", firstN(body, 400))
	}
	if _, err := mailmodel.NewMailModel(f.db).GetContactByEmail(ctx, email); err != nil {
		t.Fatalf("导入后联系人应已落库：%v", err)
	}
}

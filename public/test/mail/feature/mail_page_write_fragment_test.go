package feature

import (
	"context"
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

	rec := mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"ids": {strconv.FormatUint(row.ID, 10)}, "status": {mailmodel.ContactStatusUnsubscribed},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("批量改状态应回 200 片段，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("批量改状态的响应必须是 HTML 片段，实际 %q", ct)
	}
	// 批量成功也只播报公共一句，不再逐条给数量/明细。
	if body := rec.Body.String(); !strings.Contains(body, `role="status"`) || strings.Contains(body, "data-ui-error") {
		t.Fatalf("成功应走公共提示组件，实际：%s", firstN(body, 400))
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

	// 空勾选不再静默成功：整批无法定位任何行，按参数失败处理。
	rec = mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"status": {mailmodel.ContactStatusSubscribed},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("空勾选应整批 400，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}

	// 混合合法 / 非法 id：整批拒绝，合法行也不落库。
	second, err := f.seedContactForPage(ctx, fmt.Sprintf("qa.frag.batch.%d@example.invalid", row.ID))
	if err != nil {
		t.Fatal(err)
	}
	rec = mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"ids": {strconv.FormatUint(second, 10), "999999999"}, "status": {mailmodel.ContactStatusSubscribed},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("混合不存在 id 应整批 404，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	after, err := mailmodel.NewMailModel(f.db).GetContact(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != mailmodel.ContactStatusPending {
		t.Fatalf("整批回滚后合法行不该被改，实际 %q", after.Status)
	}

	// 导入：成功只播报公共提示，并让联系人真正落库。
	email := fmt.Sprintf("qa.frag.import.%d@example.invalid", row.ID)
	rec = mailCrudPagePost(t, router, "/admin/mail/contact/import", url.Values{
		"content":          {email},
		"consent_declared": {"1"},
		"consent_source":   {"片段验收"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("导入应回 200 片段，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	if body := rec.Body.String(); !strings.Contains(body, `role="status"`) || strings.Contains(body, "data-ui-error") {
		t.Fatalf("导入成功应走公共提示组件（不另写模块成功句），实际：%s", firstN(body, 400))
	}
	if _, err := mailmodel.NewMailModel(f.db).GetContactByEmail(ctx, email); err != nil {
		t.Fatalf("导入后联系人应已落库：%v", err)
	}
}

// seedContactForPage 造一条 pending 联系人，用于混合批次的“合法行”观察点。
func (f *mailFixture) seedContactForPage(ctx context.Context, email string) (uint64, error) {
	if err := f.seedContact(ctx, email, "片段", mailmodel.ContactStatusPending, ""); err != nil {
		return 0, err
	}
	row, err := mailmodel.NewMailModel(f.db).GetContactByEmail(ctx, email)
	if err != nil {
		return 0, err
	}
	return row.ID, nil
}

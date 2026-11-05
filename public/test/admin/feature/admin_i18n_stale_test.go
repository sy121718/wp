package feature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type i18nStaleProbe struct {
	pagecontract.PageService
	calls int
	check func(context.Context)
	err   error
}

func (p *i18nStaleProbe) MarkStaleForI18n(ctx context.Context) error {
	p.calls++
	if p.check != nil {
		p.check(ctx)
	}
	return p.err
}

// TestAdminI18nMarksStaleOnlyAfterCommittedWrites 使用真实写路由确认标记时机，而不只直调标记辅助函数。
func TestAdminI18nMarksStaleOnlyAfterCommittedWrites(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)
	probe := &i18nStaleProbe{}
	adminhttp.SetupAdminPages(fix.engine.Group("/admin"), nil, nil, nil, nil, nil, nil, probe, nil)
	t.Cleanup(func() { adminhttp.SetupAdminPages(gin.New().Group("/admin"), nil, nil, nil, nil, nil, nil, nil, nil) })
	const key = "stale.write.timing"
	for _, lang := range []string{"zh-CN", "ja-JP"} {
		if err := i18n.SaveEntry(context.Background(), i18n.Entry{Key: key, Lang: lang, Value: "保留"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		body   url.Values
		status int
		calls  int
		value  string
		absent []string
		err    error
	}{
		{"binding-failure", url.Values{"action": {"create"}}, http.StatusBadRequest, 0, "", nil, nil},
		{"missing-update", url.Values{"action": {"update"}, "key": {key}, "lang": {"en-US"}, "value": {"v0"}}, http.StatusNotFound, 0, "", nil, nil},
		{"create", url.Values{"action": {"create"}, "key": {key}, "lang": {"en-US"}, "value": {"v1"}}, http.StatusOK, 1, "v1", nil, nil},
		{"update", url.Values{"action": {"update"}, "key": {key}, "lang": {"en-US"}, "value": {"v2"}}, http.StatusOK, 1, "v2", nil, nil},
		{"invalid-bulk", url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", "invalid"}}, http.StatusBadRequest, 0, "", nil, nil},
		{"missing-bulk", url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", key + "|fr-FR"}}, http.StatusNotFound, 0, "", nil, nil},
		{"idempotent-delete", url.Values{"action": {"delete"}, "key": {key}, "lang": {"fr-FR"}}, http.StatusOK, 1, "", []string{"fr-FR"}, nil},
		{"bulk-delete", url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", key + "|ja-JP"}}, http.StatusOK, 1, "", []string{"zh-CN", "ja-JP"}, nil},
		{"delete", url.Values{"action": {"delete"}, "key": {key}, "lang": {"en-US"}}, http.StatusOK, 1, "", []string{"en-US"}, nil},
		{"marker-failure-does-not-undo-write", url.Values{"action": {"create"}, "key": {key}, "lang": {"en-US"}, "value": {"v3"}}, http.StatusOK, 1, "v3", nil, errors.New("private marker failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := probe.calls
			probe.err = tc.err
			probe.check = func(ctx context.Context) {
				if tc.value != "" {
					entry, err := i18n.GetEntry(ctx, key, "en-US")
					if err != nil {
						t.Errorf("标记时写入尚不可见：%v", err)
					} else if entry.Value != tc.value {
						t.Errorf("标记时读到旧值：got %q want %q", entry.Value, tc.value)
					}
				}
				for _, lang := range tc.absent {
					if _, err := i18n.GetEntry(ctx, key, lang); !errors.Is(err, gorm.ErrRecordNotFound) {
						t.Errorf("标记时删除尚不可见：%s err=%v", lang, err)
					}
				}
			}
			if status, _ := fix.call(t, tc.body.Encode(), true, true); status != tc.status {
				t.Fatalf("写响应状态：got %d want %d", status, tc.status)
			}
			if got := probe.calls - before; got != tc.calls {
				t.Fatalf("标记次数：got %d want %d", got, tc.calls)
			}
		})
	}
	t.Run("database-failure", func(t *testing.T) {
		before := probe.calls
		callback := fix.db.Callback().Update()
		const name = "test:i18n_stale_update_failure"
		injected := false
		if err := callback.Before("gorm:update").Register(name, func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "sys_i18n" {
				injected = true
				_ = tx.AddError(errors.New("private i18n update failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = callback.Remove(name) })
		status, _ := fix.call(t, url.Values{"action": {"update"}, "key": {key}, "lang": {"en-US"}, "value": {"rejected"}}.Encode(), true, true)
		if !injected || status != http.StatusInternalServerError || probe.calls != before {
			t.Fatalf("数据库失败不得标记：injected=%t status=%d calls=%d want=%d", injected, status, probe.calls, before)
		}
		if value, ok := fix.entry(t, key, "en-US"); !ok || value != "v3" {
			t.Fatalf("失败后原值应保留：value=%q exists=%t", value, ok)
		}
	})
}

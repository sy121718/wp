package feature

import (
	"strings"
	"testing"
)

func breakProjectsTable(t *testing.T, f *invFixture) {
	t.Helper()
	if err := f.db.Exec("ALTER TABLE projects RENAME TO projects_gone_for_load_test").Error; err != nil {
		t.Fatalf("制造装载失败（改表名）失败: %v", err)
	}
}

func assertLoadFailedPage(t *testing.T, where, body string) {
	t.Helper()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("%s 的降级渲染没有渲染完整页（缺 </html>）：模板中途中断了", where)
	}
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("%s 的降级渲染缺归口文案：页面上没有任何提示", where)
	}
	if strings.Contains(body, "uiInternalErr") {
		t.Fatalf("%s 直出了裸归口 key `MsgInternalError`（应当显示译文）", where)
	}

	if !strings.Contains(body, "这一页的数据没能读出来") {
		t.Fatalf("%s 的空态没走装载失败档（判据 LoadFailed 没传给模板）", where)
	}
	assertNoInternalTokens(t, where, body)
}

func TestPageErrI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		"admin.common.list.loadFailedTitle",
		"admin.common.list.loadFailedDesc",
		"tplDepsMiss",
		"uiInternalErr",
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE lang = ? AND item_key IN (?)",
			lang, keys).Scan(&n).Error; err != nil {
			t.Fatalf("查 %s 词条失败: %v", lang, err)
		}
		if n != int64(len(keys)) {
			t.Errorf("%s 词条 %d 条，期望 %d 条（缺的那条在页面上会显示裸 key 或回落另一种语言）",
				lang, n, len(keys))
		}
	}
}

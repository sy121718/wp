package projecthttp

// project_err_i18n_test.go — 页面提示文案的**词条登记**对账（静态检查，不连库）。
//
// 为什么单独守这一条：projectPageErrKeys 里的 key 会被 r.TranslateMessage 翻成页面提示，
// 而「键存在但词条没登记」的表现是**页面显示裸 key**（如 `ErrThemeIDRequired` 直接摆给运营看）——
// 不报错、不记日志，只有人眼能发现。实测踩过一次同族问题：`c.String(500, "MsgInternalError")`
// 渲染出英文 key，整批页面都是这个样。
//
// 判据用**迁移 SQL 里出现过这个 key**而不是「库里有行」：迁移是词条的唯一登记处
// （新增词条必须写迁移，见 AGENTS.md「数据库」），而测试库不跑 seed，
// 连库断言会把「seed 没跑」误判成「词条缺失」。
//
// 它是启发式的：key 出现在注释里也算命中。漏报（注释里提到但没真插）留给
// 「页面显示裸 key」这个一眼可见的症状兜底 —— 静态检查抓的是「完全没登记」。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// migrationsDir 从本测试文件出发定位 public/migrations。
const migrationsDir = "../../../../../public/migrations"

// TestProjectPageErrKeysAreRegisteredInMigrations 每个页面提示 key 都必须在迁移 SQL 里登记。
func TestProjectPageErrKeysAreRegisteredInMigrations(t *testing.T) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("读取迁移目录失败（路径假设变了要同步改）: %v", err)
	}
	var corpus strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if rerr != nil {
			t.Fatalf("读 %s 失败: %v", e.Name(), rerr)
		}
		corpus.Write(b)
		corpus.WriteString("\n")
	}
	all := corpus.String()
	if len(all) == 0 {
		t.Fatal("迁移语料为空：路径写错时这条测试会变成永远通过的空检查")
	}
	for _, key := range projectPageErrKeys {
		if !strings.Contains(all, "'"+key+"'") {
			t.Errorf("文案 key %q 没有出现在任何迁移 SQL 里 —— 页面会直接显示这个裸 key（新增词条要写迁移）", key)
		}
	}
}

// TestProjectErrRedirectEscapesText 回跳 URL 必须转义文案，且自带 query 时用 & 连接。
//
// 这两点错了的症状都不明显：不转义的文案里 `&` / `#` 会把后面的参数截断
// （提示静默消失），分隔符用错则是 `?err=` 变成路径的一部分（回跳到不存在的 URL）。
func TestProjectErrRedirectEscapesText(t *testing.T) {
	cases := []struct {
		back, text string
		wantSub    string
	}{
		{"/admin/themes", "主题不存在", "/admin/themes?err="},
		{"/admin/themes?project=p1", "主题不存在", "/admin/themes?project=p1&err="},
		{"/admin/settings?project=p1", "工程与站点名称不能为空", "/admin/settings?project=p1&err="},
	}
	for _, tc := range cases {
		got := buildErrRedirectURL(tc.back, tc.text)
		if !strings.HasPrefix(got, tc.wantSub) {
			t.Errorf("buildErrRedirectURL(%q, %q) = %q，期望以 %q 开头", tc.back, tc.text, got, tc.wantSub)
		}
		if strings.Contains(got, tc.text) {
			t.Errorf("文案未转义：%q 里出现了原文 %q", got, tc.text)
		}
	}
}

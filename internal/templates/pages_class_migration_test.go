package templates

// pages_class_migration_test.go — 后台模板旧桥接类 `pages-*` 的迁移契约（审计 UI-019 / UI-020）。
//
// 邮件六个页面与 locale 行片段原先靠 theme.css 的一组容器类上样式：
// .pages-wrap / .pages-card / .pages-table / .pages-badge(-ok/-warn) / .pages-form / .pages-empty。
// 这套类属于「桥接」——只为存量，不再扩大使用面（见 docs/02-F-ui-kit.md §10.4）。
// 视觉的唯一来源是 ui.css 的公共类，迁移后的映射：
//
//	pages-wrap            → stack
//	pages-card card       → card card-body
//	pages-table data-table→ data-table（此前已是死代码：ui.css 的 table.data-table 特异性更高）
//	pages-badge           → badge badge-mute
//	pages-badge-ok        → badge badge-success
//	pages-badge-warn      → badge badge-warning
//	pages-form form-row   → form-row items-center（items-center 必须保留：grid 里没有它，
//	                        卡片与按钮会被 stretch 拉高，实测按钮 37px → 58px）
//	pages-empty           → hint
//
// 钉住「admin 模板里不再出现 pages-* 类」的理由：旧类与公共类都定义了一份，
// 同时挂在元素上时谁生效取决于加载顺序与 :not 堆出来的特异性（theme.css 在前、ui.css 在后），
// 回退一处就会形成「改了但看不出哪条规则在起作用」的形态，是视觉回归最难定位的那类。
import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// legacyPagesClassRe 匹配 class 属性里出现的 pages-* 类（限定在 class 内，避免误伤注释）。
var legacyPagesClassRe = regexp.MustCompile(`class="[^"]*pages-[a-z0-9-]+`)

func TestAdminTemplatesHaveNoLegacyPagesClasses(t *testing.T) {
	var files []string
	err := filepath.WalkDir("admin", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".html") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 admin 目录失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("admin 目录下没有找到 .html 模板，测试断言失去意义")
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		if hits := legacyPagesClassRe.FindAllString(string(data), -1); len(hits) > 0 {
			t.Errorf("%s 仍有旧桥接类 %v；应改用 ui.css 公共类（stack / card card-body / data-table / "+
				"badge badge-mute|success|warning / form-row items-center / hint）", path, hits)
		}
	}
}

// TestMailAdminTemplatesUseSharedClasses 是上一条的反向断言：
// 只删旧类但不写新类，同样会掉样式（而且比原来更差）。这里要求邮件相关页面
// 确实用上了迁移后的公共类。
func TestMailAdminTemplatesUseSharedClasses(t *testing.T) {
	pages := []string{
		"admin/mail/mail.html",
		"admin/mail/mail_templates.html",
		"admin/mail/mail_contacts.html",
		"admin/mail/mail_campaigns.html",
		"admin/mail/mail_campaign.html",
		"admin/mail/mail_automation.html",
		"admin/mail/mail_automation_runs.html",
		"admin/mail/mail_automation_edit.html",
		"admin/mail/mail_automation_run.html",
	}
	for _, page := range pages {
		data, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", page, err)
		}
		body := string(data)
		if !strings.Contains(body, `<div class="stack">`) {
			t.Errorf("%s 缺少公共类 <div class=\"stack\">（旧类被删除后此处会掉样式）", page)
		}
		// 页面壳要么是「内容卡」（card card-body，有说明/表单区块），
		// 要么是「列表卡」（card list-card，纯列表页 —— 见 admin-ui-logic §7 标准骨架）。
		// 两者都是合规形态，早期只认前者会让纯列表页误报。
		if !strings.Contains(body, `class="card card-body"`) && !strings.Contains(body, `class="card list-card"`) {
			t.Errorf("%s 既没有 card card-body 也没有 card list-card（页面缺少卡片壳）", page)
		}
	}
}

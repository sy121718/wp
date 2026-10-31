package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

func TestMailAdminTemplatesUseSharedClasses(t *testing.T) {
	pages := []string{
		"admin/mail/mail.html",
		"admin/mail/mail_templates.html",
		"admin/mail/mail_contacts.html",
		"admin/mail/mail_campaigns.html",
		"admin/mail/mail_campaign.html",
		"admin/mail/mail_automation.html",
		"admin/mail/mail_automation_runs.html",
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

		if !strings.Contains(body, `class="card card-body"`) && !strings.Contains(body, `class="card list-card"`) {
			t.Errorf("%s 既没有 card card-body 也没有 card list-card（页面缺少卡片壳）", page)
		}
	}
}

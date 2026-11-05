package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	legacyFlashRe       = regexp.MustCompile(`[\s"]data-write-flash(=|[\s>/])`)
	legacyFlashTargetRe = regexp.MustCompile(`[\s"]data-write-flash-target(=|[\s>/])`)
	fullWriteFormRe     = regexp.MustCompile(`<form\b[^>]*data-write-reload[^>]*>`)
)

func TestAdminWriteFlashTargetsPaired(t *testing.T) {
	root := filepath.Join("admin")
	var legacy []string
	var reloadOutsideForm []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}

		text := stripJetComments(string(src))
		if legacyFlashRe.MatchString(text) || legacyFlashTargetRe.MatchString(text) {
			legacy = append(legacy, path)
		}

		for _, m := range fullWriteFormRe.FindAllString(text, -1) {
			// 写表单的判据是「声明了 HTMX 写方法」：data-write-url 已退役，
			// 只剩 data-write-reload 而没有 hx-post/put/patch/delete 的表单不会真的提交。
			if !strings.Contains(m, "hx-post=") && !strings.Contains(m, "hx-put=") &&
				!strings.Contains(m, "hx-patch=") && !strings.Contains(m, "hx-delete=") {
				reloadOutsideForm = append(reloadOutsideForm, path+"："+m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}

	legacyAllow := map[string]bool{}

	for _, p := range legacy {
		if legacyAllow[p] {
			continue
		}
		t.Errorf("%s: 仍在用已下线的 data-write-flash / data-write-flash-target —— "+
			"新基座只认 data-write-reload（结论经 sessionStorage 跨刷新），旧属性不会生效，回执会静默丢失", p)
	}
	for _, p := range reloadOutsideForm {
		t.Errorf("%s: data-write-reload 出现在没有 HTMX 写方法的 form 上 —— 它不是写表单，刷新永不触发", p)
	}
}

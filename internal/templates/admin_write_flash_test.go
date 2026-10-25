package templates

// admin_write_flash_test.go — 后台模板「跨刷新的结论」在**新写基座**下的判据。
//
// 契约变更（2026-11，写基座统一）：旧契约靠两个属性跨文件配对 ——
// `data-write-flash="name"` 在写成功时存结论，`data-write-flash-target="name"`
// 在目标页显示它。两半靠同一个字符串对齐，拼错任一半都不报错，所以要门禁守。
//
// 新基座（ui/write-base.js）把这件事收成**一个属性**：`data-write-reload`。
// 基座在 reload 前把服务端结论写进 sessionStorage 的固定键 `wbui:write-flash`，
// 页面载入时读一次即删。没有第二个半、没有可拼错的字符串 —— 因此旧的双向配对判据
// 不再适用（它守的失效模式已经不存在）。
//
// 仍然值得守的：
//  1. 旧的 flash / flash-target 属性**不许回来**（回来就意味着有人写了两套机制，
//     而新基座不认旧属性 → 结论静默丢失）；
//  2. `data-write-reload` 只出现在有 data-write-url 的表单上（裸属性挂错地方没有意义）。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 只匹配**真正的属性用法**：位于标签内（前面是空白或引号），后接 `=` 或属性边界。
//
// 不能只按名字匹配：注释里会写「原来的 data-write-flash-target 是…」记录历史，
// 那不是在使用旧属性。这里用「前导空白 + 名字 + (= 或 空白/> )」限定在标签内。
var (
	legacyFlashRe       = regexp.MustCompile(`[\s"]data-write-flash(=|[\s>/])`)
	legacyFlashTargetRe = regexp.MustCompile(`[\s"]data-write-flash-target(=|[\s>/])`)
	fullWriteFormRe     = regexp.MustCompile(`<form\b[^>]*data-write-reload[^>]*>`)
)

// TestAdminWriteFlashTargetsPaired 旧的 flash 配对属性已下线，新机制只有一个属性。
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
		// 先剥掉 Jet 注释 {* … *} 再判：注释里写「原来的 data-write-flash-target 是…」
		// 是在记录历史，不是在用旧属性。不剥的话每条历史说明都会被当成违规。
		text := stripJetComments(string(src))
		if legacyFlashRe.MatchString(text) || legacyFlashTargetRe.MatchString(text) {
			legacy = append(legacy, path)
		}
		// data-write-reload 出现在 form 标签里时，该 form 必须同时有 data-write-url：
		// 否则它不是一个写表单，"成功后刷新" 永远不会触发。
		for _, m := range fullWriteFormRe.FindAllString(text, -1) {
			if !strings.Contains(m, "data-write-url=") {
				reloadOutsideForm = append(reloadOutsideForm, path+"："+m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}

	// 存量豁免：这些文件在本批改写前就带旧属性，逐个清理；清单只减不增。
	// 当前为空 = 已全部清理。
	legacyAllow := map[string]bool{}

	for _, p := range legacy {
		if legacyAllow[p] {
			continue
		}
		t.Errorf("%s: 仍在用已下线的 data-write-flash / data-write-flash-target —— "+
			"新基座只认 data-write-reload（结论经 sessionStorage 跨刷新），旧属性不会生效，回执会静默丢失", p)
	}
	for _, p := range reloadOutsideForm {
		t.Errorf("%s: data-write-reload 出现在没有 data-write-url 的 form 上 —— 它不是写表单，刷新永不触发", p)
	}
}

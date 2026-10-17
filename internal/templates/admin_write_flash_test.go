package templates

// admin_write_flash_test.go — 后台模板「跨刷新的结论」双向配对。
//
// ui/json_write.js 的 data-write-flash 契约：写成功后在 storeFlash(name) 里记一条结论，
// 紧接着 reload；页面载入时 renderFlash 找到 [data-write-flash-target="name"] 把它显示一次。
// 两半靠**同一个字符串**对齐，任何一半拼错都不报错：
//
//   - 写了 flash 却没有 target → 结论什么都不显示，用户以为操作失败（列表还刷新了）；
//   - 写了 target 却没有 flash → 那块区域永远空着，占位可见而内容永不到来。
//
// 两者都是「页面看着正常、只是那句回执没了」。配对允许跨文件（A 页写、跳过去的 B 页读），
// 所以判据是**全局集合双向包含**，不是逐文件。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	writeFlashRe       = regexp.MustCompile(`data-write-flash="([^"]+)"`)
	writeFlashTargetRe = regexp.MustCompile(`data-write-flash-target="([^"]+)"`)
)

// TestAdminWriteFlashTargetsPaired data-write-flash 与 data-write-flash-target 双向配对。
func TestAdminWriteFlashTargetsPaired(t *testing.T) {
	root := filepath.Join("admin")
	flashes := map[string]string{} // name → 首个声明它的文件
	targets := map[string]string{}

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		src := string(body)
		for _, m := range writeFlashRe.FindAllStringSubmatch(src, -1) {
			if _, ok := flashes[m[1]]; !ok {
				flashes[m[1]] = rel
			}
		}
		for _, m := range writeFlashTargetRe.FindAllStringSubmatch(src, -1) {
			if _, ok := targets[m[1]]; !ok {
				targets[m[1]] = rel
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if len(flashes) == 0 && len(targets) == 0 {
		t.Fatal("没有扫到任何 data-write-flash / data-write-flash-target：模板约定或本测试的匹配串已经漂移")
	}

	for name, f := range flashes {
		if _, ok := targets[name]; !ok {
			t.Errorf("%s: data-write-flash=%q 没有对应的 data-write-flash-target —— "+
				"写成功后的结论不会显示在任何地方（列表照旧刷新，用户读不到「做了什么」）", f, name)
		}
	}
	for name, f := range targets {
		if _, ok := flashes[name]; !ok {
			t.Errorf("%s: data-write-flash-target=%q 没有对应的 data-write-flash —— "+
				"这块结论区永远是空的", f, name)
		}
	}
	t.Logf("结论区配对：flash %d 个 / target %d 个", len(flashes), len(targets))
}

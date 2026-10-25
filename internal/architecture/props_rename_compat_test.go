package architecture

// props_rename_compat_test.go — 组件 props 改名的兼容门禁。
//
// 守的缺陷（实测过一次，会静默删内容）：props 改名后，存量文档里的旧键被忽略 →
// 字段为空 → 组件里「有没有这一块」的判断为假 → 整块内容不渲染。不报错、不 500，
// 产物里只是少一颗按钮 / 一段文案。
//
// 本仓实测影响面：blueprints 2 张、content_templates 1 张、document_snapshots
// 5.78 万条含旧键、97 个已发布实例。
//
// 判据（两条，任一满足即放行）：
//
//  1. 组件在 `core.LegacyKey` 表里声明了旧名 → 归一自动生效（推荐，不动数据）；
//  2. 该组件的 Props 里没有出现过"曾经的旧名" → 无需兼容。
//
// 换句话说：**每个组件的 json tag 一旦出现过、又消失了，就必须在 legacy 表里留痕**。
// 这条测试靠一张历史旧名清单来判定（见 knownRenamedProps），清单只增不减 ——
// 它记录的是"这个项目曾经用过的名字"，与代码是否还在用无关。

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// knownRenamedProps 组件 props 的历史旧名（JSON 键）。
//
// 每条都必须被对应组件的 LegacyKey 表覆盖，否则旧文档静默丢内容。
// **只增不减**：名字一旦在产物里出现过，就永远有人在用自己的存档。
var knownRenamedProps = map[string][]string{
	// 这一批：三个组件的按钮/链接文案统一成 label。
	"internal/builder/components/card/card.go":               {"buttonText"},
	"internal/builder/components/infobox/infobox.go":         {"btnText"},
	"internal/builder/components/addtocart/addtocart.go":     {"buttonText"},
	"internal/builder/components/articlelist/articlelist.go": {"linkText"},
}

// legacyKeysRe 抓 LegacyKey 表里的 From/To 对。
var legacyKeysRe = regexp.MustCompile(`\{From:\s*"([^"]+)",\s*To:\s*"([^"]+)"\}`)

func TestRenamedPropsHaveLegacyMapping(t *testing.T) {
	root := repoRoot(t)

	for file, oldNames := range knownRenamedProps {
		path := filepath.Join(root, file)
		src, err := parseGoFile(path)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", file, err)
		}

		// 现行 json tag（用来断言「旧名确实已经不在 tag 里了」——
		// 如果它还在 tag 里，说明改名没做完，这条门禁的前提不成立）。
		current := jsonTagsOf(src)
		for _, old := range oldNames {
			if current[old] {
				t.Errorf("%s：旧名 %q 仍在 json tag 里 —— 改名没做完（要么改完，要么从 knownRenamedProps 删掉）", file, old)
			}
		}

		// 声明了归一吗？
		declared := map[string]bool{}
		for _, m := range legacyKeysRe.FindAllStringSubmatch(src, -1) {
			declared[m[1]] = true
		}
		for _, old := range oldNames {
			if !declared[old] {
				t.Errorf("%s：旧名 %q 没有 LegacyKey 声明 —— 存量文档里带这个键的节点会**静默丢内容**"+
					"（不报错、不 500，产物里只是少一块）。修法：在该组件的 xxxLegacyKeys 加 "+
					"`{From: %q, To: <新名>}`，或写数据迁移把库里这些键改掉。",
					file, old, old)
			}
		}
	}
}

// TestLegacyKeysReMatchesRealDeclarations 自检：正则确实能扫到真实声明。
// 抓不到任何一条时，上面那条会在空集上「全部通过」——那是最危险的一种绿。
func TestLegacyKeysReMatchesRealDeclarations(t *testing.T) {
	root := repoRoot(t)
	total := 0
	for file := range knownRenamedProps {
		src, err := parseGoFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", file, err)
		}
		total += len(legacyKeysRe.FindAllStringSubmatch(src, -1))
	}
	if total == 0 {
		t.Fatal("一处 LegacyKey 声明都没扫到：正则或路径口径失效，上一条门禁在空转")
	}
}

// parseGoFile 读源码为字符串。
//
// 先用 AST 校验可解析（文件语法坏了要在这里就报，而不是让正则静默扫不到），
// 再返回原文用正则找字面量表 —— 「找一张表的全部条目」用正则可读性远好于走 AST。
func parseGoFile(path string) (string, error) {
	if _, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

// jsonTagsOf 收集 struct 字段的 json 键名。
func jsonTagsOf(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`json:"([A-Za-z][A-Za-z0-9_]*)[,"]`).FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	return out
}

package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var knownRenamedProps = map[string][]string{

	"internal/builder/components/card/card.go":               {"buttonText"},
	"internal/builder/components/infobox/infobox.go":         {"btnText"},
	"internal/builder/components/addtocart/addtocart.go":     {"buttonText"},
	"internal/builder/components/articlelist/articlelist.go": {"linkText"},
}

var legacyKeysRe = regexp.MustCompile(`\{From:\s*"([^"]+)",\s*To:\s*"([^"]+)"\}`)

func TestRenamedPropsHaveLegacyMapping(t *testing.T) {
	root := repoRoot(t)

	for file, oldNames := range knownRenamedProps {
		path := filepath.Join(root, file)
		src, err := parseGoFile(path)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", file, err)
		}

		current := jsonTagsOf(src)
		for _, old := range oldNames {
			if current[old] {
				t.Errorf("%s：旧名 %q 仍在 json tag 里 —— 改名没做完（要么改完，要么从 knownRenamedProps 删掉）", file, old)
			}
		}

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

func parseGoFile(path string) (string, error) {
	if _, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func jsonTagsOf(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`json:"([A-Za-z][A-Za-z0-9_]*)[,"]`).FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	return out
}

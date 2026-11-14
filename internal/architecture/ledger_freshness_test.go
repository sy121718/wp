package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

// 以下台账的路径键在搬包后可能永久失效；任何一条指向不存在的文件都说明
// 门禁已在空转，必须显式失败而不是静默通过。
var budgetLedgers = []struct {
	name    string
	entries map[string]int
}{
	{name: "htmlLiteralBudget", entries: htmlLiteralBudget},
	{name: "pendingMsgTextDomain", entries: pendingMsgTextDomain},
	{name: "pendingKeepEmptyFields", entries: pendingKeepEmptyFields},
}

// TestBudgetLedgerPathsExist 防止包移动后旧路径键永久失效。
func TestBudgetLedgerPathsExist(t *testing.T) {
	root := repoRoot(t)
	for _, ledger := range budgetLedgers {
		var stale []string
		for file := range ledger.entries {
			if _, err := os.Stat(filepath.Join(root, file)); err != nil {
				stale = append(stale, file)
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			t.Errorf("%s 存在失效路径（文件已不存在，条目应在迁移时同步删除或改名）：\n%s",
				ledger.name, strings.Join(stale, "\n"))
		}
	}
}

// TestScanRootsExist 保证每个门禁的扫描根真实存在，路径写错直接失败。
func TestScanRootsExist(t *testing.T) {
	root := repoRoot(t)
	roots := []string{
		"internal/module",
		"internal/routers",
		"internal/templates",
		"pkg",
		// 写端点字段门禁的三个模板属主：应用模板、公共 UI、工具模板资产。
		// 缺任一个都会让该属主下的表单静默逃过对账。
		"pkg/view/ui/templates",
		"pkg/builder",
	}
	for _, rel := range roots {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("扫描根 %s 不存在：%v", rel, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("扫描根 %s 不是目录", rel)
		}
	}
}

// TestScanRootsAreNotEmpty 区分「违规为零」与「源文件为零」：
// 预期应有输入的扫描根一旦为空，必须显式失败。
func TestScanRootsAreNotEmpty(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		root string
		ext  string
		min  int
	}{
		{root: "pkg", ext: ".go", min: 10},
		{root: filepath.Join("internal", "module"), ext: ".go", min: 100},
		{root: filepath.Join("internal", "templates"), ext: ".html", min: 50},
		{root: filepath.Join("pkg", "view", "ui", "templates"), ext: ".html", min: 5},
		{root: filepath.Join("pkg", "builder"), ext: ".jet", min: 5},
	}
	for _, c := range cases {
		count := 0
		err := filepath.WalkDir(filepath.Join(root, c.root), func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, c.ext) {
				count++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败：%v", c.root, err)
		}
		if count < c.min {
			t.Errorf("扫描根 %s 只匹配到 %d 个 %s 文件（预期至少 %d）：扫描可能已经空转",
				c.root, count, c.ext, c.min)
		}
	}
}

// TestWriteContractArtifactPresent 保证字段门禁的数据源真实存在且有内容。
//
// 空契约或生成物缺失会让所有提交入口「不在契约里」，也可能被误当成全部对账；
// 两种情况都不能算门禁通过。
func TestWriteContractArtifactPresent(t *testing.T) {
	root := repoRoot(t)
	rel := "internal/templates/static/js/ui/write-contracts.json"
	path := filepath.Join(root, rel)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("写端点契约生成物缺失：%v（先跑 go run ./cmd/write-contracts）", err)
	}
	if info.Size() == 0 {
		t.Fatalf("写端点契约生成物为空：%s", rel)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取写端点契约生成物失败：%v", err)
	}
	var contracts map[string]struct{}
	if err := json.Unmarshal(b, &contracts); err != nil {
		t.Fatalf("写端点契约生成物不是合法 JSON 对象：%v", err)
	}
	if len(contracts) == 0 {
		t.Fatal("写端点契约没有条目：字段门禁会空转，不能算已对账")
	}
}

// TestWriteContractDTOsExist 校验生成物里的 DTO 仍能在源码中找到。
//
// 只校验「文件存在且非空」挡不住生成物整体过期：DTO 改名/删字段后契约仍会带着
// 旧名放行模板。这里把每条契约声明的 DTO 反查回源码目录，缺一个就说明生成物
// 需要重跑 `go run ./cmd/write-contracts`，或该端点已退役。
func TestWriteContractDTOsExist(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal/templates/static/js/ui/write-contracts.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取写端点契约失败：%v", err)
	}
	var contracts map[string]struct {
		DTO     string `json:"dto"`
		Handler string `json:"handler"`
	}
	if err := json.Unmarshal(b, &contracts); err != nil {
		t.Fatalf("写端点契约不是合法 JSON 对象：%v", err)
	}
	if len(contracts) == 0 {
		t.Fatal("写端点契约没有条目")
	}

	// DTO 是 `<包路径>.Type`：只取最后一段类型名，在 internal/ 与 pkg/ 的声明里反查。
	// 这里不做包级精确匹配：契约里的包路径是按扫描前缀拼的，类型名才是唯一稳定锚点。
	typeDecls := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "pkg"} {
		for _, path := range goFilesUnder(t, filepath.Join(root, dir), func(string) bool { return true }) {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				continue
			}
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range gd.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						typeDecls[ts.Name.Name] = true
					}
				}
			}
		}
	}

	var missing []string
	for key, spec := range contracts {
		if spec.DTO == "" {
			continue
		}
		name := spec.DTO[strings.LastIndex(spec.DTO, ".")+1:]
		if !typeDecls[name] {
			missing = append(missing, key+" → "+spec.DTO)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("写端点契约引用了源码里已不存在的 DTO（生成物过期，重跑 go run ./cmd/write-contracts）：\n%s",
			strings.Join(missing, "\n"))
	}
}

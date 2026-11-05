package templates

import (
	jetpkg "go_wp/pkg/view/jet"
	uipkg "go_wp/pkg/view/ui"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// templateTemplateRef 匹配 Jet 的 include / extends / import 静态引用。
var templateTemplateRef = regexp.MustCompile(`\{\{\s*(?:include|extends|import)\s+"([^"]+)"`)

// releaseTemplateRoots 是 release loader 必须覆盖的模板逻辑根。
// 应用侧磁盘目录与 pkg/view/ui 的公共 UI 目录共同组成发布模板集。
var releaseTemplateRoots = []string{"admin", "workbench", "fragments"}

// releaseTemplateLayer 是一层模板源：应用磁盘目录或 pkg/view/ui 的 embed。
type releaseTemplateLayer struct {
	name string
	fsys fs.FS
}

// TestReleaseLoaderCoversAllPageTemplates 守住 P0 缺陷：
// 生产 embed 必须能按逻辑键打开后台完整布局、根 partials、workbench 与 HTML fragments。
func TestReleaseLoaderCoversAllPageTemplates(t *testing.T) {
	loader, err := newAdminTemplateLoader()
	if err != nil {
		t.Fatalf("构建 release loader 失败：%v", err)
	}

	for _, rel := range pageTemplateFiles(t) {
		if !loader.Exists(rel) {
			t.Errorf("release loader 缺少模板 %s（生产环境将回退或渲染失败）", rel)
		}
	}
}

// TestReleaseLoaderResolvesTemplateReferences 逐条打开 include/extends/import 引用的逻辑键。
func TestReleaseLoaderResolvesTemplateReferences(t *testing.T) {
	loader, err := newAdminTemplateLoader()
	if err != nil {
		t.Fatalf("构建 release loader 失败：%v", err)
	}

	var missing []string
	for _, rel := range pageTemplateFiles(t) {
		raw := layeredTemplateSource(t, rel)
		for _, m := range templateTemplateRef.FindAllStringSubmatch(stripJetComments(raw), -1) {
			target := resolveLogicalKey(rel, m[1])
			if !templateKeyExists(loader, target) {
				missing = append(missing, rel+" -> "+target)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("release loader 无法解析以下模板引用：\n%s", strings.Join(missing, "\n"))
	}
}

// templateLayers 返回应用磁盘模板与 pkg/view/ui 公共模板两层源。
func templateLayers(t *testing.T) []releaseTemplateLayer {
	t.Helper()
	return []releaseTemplateLayer{
		{name: "app", fsys: os.DirFS(".")},
		{name: "pkg/view/ui", fsys: uipkg.TemplatesFS()},
	}
}

// pageTemplateFiles 列出全部发布模板逻辑键（两层源、逻辑键同形）。
func pageTemplateFiles(t *testing.T) []string {
	t.Helper()
	roots := append(append([]string{}, releaseTemplateRoots...), "admin/partials", "partials")
	seen := map[string]bool{}
	var files []string
	for _, layer := range templateLayers(t) {
		for _, root := range roots {
			err := fs.WalkDir(layer.fsys, root, func(p string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					if os.IsNotExist(walkErr) || walkErr == fs.ErrNotExist {
						return nil
					}
					return walkErr
				}
				if d.IsDir() || (!strings.HasSuffix(p, ".html") && !strings.HasSuffix(p, ".jet")) {
					return nil
				}
				if seen[p] {
					return nil
				}
				seen[p] = true
				files = append(files, p)
				return nil
			})
			if err != nil {
				t.Fatalf("遍历模板根 %s（%s）失败：%v", root, layer.name, err)
			}
		}
	}
	if len(files) == 0 {
		t.Fatal("没有找到任何模板文件，扫描根可能写错了")
	}
	return files
}

// readTemplateSource 按逻辑键从任一层源读模板正文。
func layeredTemplateSource(t *testing.T, name string) string {
	t.Helper()
	for _, layer := range templateLayers(t) {
		if b, err := fs.ReadFile(layer.fsys, name); err == nil {
			return string(b)
		}
	}
	t.Fatalf("两层源都读不到模板 %s", name)
	return ""
}

// templateKeyExists 按 Jet 的扩展名补全规则判断逻辑键是否存在。
func templateKeyExists(loader *jetpkg.FSLoader, key string) bool {
	if loader.Exists(key) {
		return true
	}
	for _, ext := range []string{".jet", ".html"} {
		if loader.Exists(key + ext) {
			return true
		}
	}
	return false
}

// resolveLogicalKey 把相对引用折算成模板逻辑键。
func resolveLogicalKey(fromFile, ref string) string {
	if strings.HasPrefix(ref, "/") {
		return strings.TrimPrefix(path.Clean(ref), "/")
	}
	base := path.Dir(fromFile)
	return strings.TrimPrefix(path.Clean(path.Join(base, ref)), "/")
}

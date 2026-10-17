package routers

// write_endpoint_contract_test.go — 后台模板的 JSON 写端点与路由表对账。
//
// 为什么需要它：`data-write-api` 是**字符串**，模板侧写错一个字母不会有任何编译错，
// 运行时症状是「按钮点下去没反应」（fetch 404，基座把失败写进结论行，而结论行一开始
// 是空的 —— 看起来像点了没生效）。本仓已经发生过两次真实故障：
//
//   - C 批把 endpoint 注册在根组（`/api/translations/save` 而不是 `/api/product/translations/save`），
//     只有重写 routes.snapshot 才暴露；
//   - E 批删掉 6 条页面写路由，若模板里还留着引用，前端就 404 —— 判断「模板有没有引用」
//     当时靠 rg 人肉看。
//
// 判据三条（都是「不报错但行为错」的形状，所以必须机器守）：
//
//  1. 模板声明的写端点必须在 testdata/routes.snapshot 里且方法为 POST；
//  2. `data-write-mode` 取值必须是基座真的实现了的那四档 —— 写错档位时基座**静默降级**
//     成 single（只发 id，其余字段全丢），端点收到一个「看着有 id 但什么都没改」的身体；
//  3. 带 data-write-mode 的按钮必须带 `data-requires-js`：无 JS 时模板里这些按钮是
//     hidden + disabled，靠基座起来后摘掉；漏写这一个属性 = 无 JS 环境下按钮可见可点，
//     点下去什么都不会发生。
//
// 只读文件，不需要数据库（不装配路由）：快照就是路由表的权威复本，模板改了没重写
// 快照的漂移由 TestRouteSnapshotStable 负责抓。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	writeAPIRe   = regexp.MustCompile(`data-write-(?:api|url)="([^"]+)"`)
	writeModeRe  = regexp.MustCompile(`data-write-mode="([^"]+)"`)
	buttonTagRe  = regexp.MustCompile(`(?s)<button\b[^>]*>`)
	jetCondWrite = regexp.MustCompile(`^(.*?)\{\{if[^}]*\}\}([^{}]*)\{\{else\}\}([^{}]*)\{\{end\}\}(.*)$`)
)

// writeModesSupported 基座 ui/json_write.js 真的实现了的档位（见该文件头部的契约）。
var writeModesSupported = map[string]bool{"single": true, "bulk": true, "create": true, "form": true}

// loadRouteSnapshot 读路由快照成 "METHOD PATH" 集合。
func loadRouteSnapshot(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "routes.snapshot"))
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			set[line] = true
		}
	}
	if len(set) == 0 {
		t.Fatal("路由快照为空")
	}
	return set
}

// loadAdminTemplates 读全部后台模板（相对路径 → 内容）。
func loadAdminTemplates(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "templates", "admin")
	files := map[string]string{}
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
		files[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("后台模板目录为空：%s", root)
	}
	return files
}

// expandWriteEndpoints 把一个 data-write-api 值展开成具体路径。
//
// 模板里有两处用 Jet 条件在 create / update 之间切换（标签、分类、属性组表单），
// 展开成两个路径分别对账。出现别的动态形态（纯变量、循环变量）时判失败：
// 那种写法的端点无法对账，等于把这条闸门关掉了。
func expandWriteEndpoints(rel, value string) []string {
	if m := jetCondWrite.FindStringSubmatch(value); m != nil {
		return []string{m[1] + m[2] + m[4], m[1] + m[3] + m[4]}
	}
	if strings.Contains(value, "{{") || !strings.HasPrefix(value, "/") {
		return nil
	}
	return []string{value}
}

// TestWriteAPIEndpointsExistInRouteTable 模板声明的每个写端点都必须在路由表里。
func TestWriteAPIEndpointsExistInRouteTable(t *testing.T) {
	routes := loadRouteSnapshot(t)
	var missing, dynamic, total int
	for rel, body := range loadAdminTemplates(t) {
		for _, m := range writeAPIRe.FindAllStringSubmatch(body, -1) {
			eps := expandWriteEndpoints(rel, m[1])
			if len(eps) == 0 {
				dynamic++
				t.Errorf("%s: data-write-api=%q 含无法对账的动态值", rel, m[1])
				continue
			}
			for _, ep := range eps {
				total++
				if !routes["POST "+ep] {
					missing++
					t.Errorf("%s: 写端点 %s 不在路由表（基座 fetch 会 404，按钮点了没反应）", rel, ep)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("没有扫到任何 data-write-api：模板约定或本测试的匹配串已经漂移")
	}
	t.Logf("写端点对账：%d 个（动态 %d 个），缺失 %d 个", total, dynamic, missing)
}

// TestWriteModeValuesAreSupported data-write-mode 只能取基座实现了的四档。
func TestWriteModeValuesAreSupported(t *testing.T) {
	for rel, body := range loadAdminTemplates(t) {
		for _, m := range writeModeRe.FindAllStringSubmatch(body, -1) {
			if !writeModesSupported[m[1]] {
				t.Errorf("%s: data-write-mode=%q 不是基座支持的档位（single/bulk/create/form）——"+
					"写错时基座静默降级成 single，只发 id，其余字段全丢", rel, m[1])
			}
		}
	}
}

// TestWriteButtonsRequireJS 带 data-write-mode 的按钮必须带 data-requires-js。
//
// 无 JS 时这些按钮在模板里是 hidden + disabled，由基座起来后摘掉；漏写这个属性，
// 无 JS 环境下按钮可见可点，点下去什么都不会发生（表单提交被 data-write-api 的 JS 交接
// 假定接管，而 JS 根本不在）。
func TestWriteButtonsRequireJS(t *testing.T) {
	var checked int
	for rel, body := range loadAdminTemplates(t) {
		for _, tag := range buttonTagRe.FindAllString(body, -1) {
			if !strings.Contains(tag, "data-write-mode=") {
				continue
			}
			checked++
			if !strings.Contains(tag, "data-requires-js") {
				flat := strings.Join(strings.Fields(tag), " ")
				t.Errorf("%s: 写按钮缺 data-requires-js：%s", rel, flat)
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到带 data-write-mode 的按钮：模板约定或本测试的匹配串已经漂移")
	}
	t.Logf("写按钮防呆：%d 个", checked)
}

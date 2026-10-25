package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 前后端分界：Go 是后端，Jet 模板 + HTML 是前端。后端代码里出现 HTML 标签就是越界 ——
// 它意味着「渲染」这件事发生在 Go 里，前端层（internal/templates）再也管不到那段结构。
//
// 判据只看**字符串字面量**（ast 的 BasicLit），注释天然不算。扫描范围是模块代码与路由装配处。
//
// 允许的三类「不是参与 HTML」：
//  1. 交视图数据（控制器 return 视图，由 adminui.Page / Bare 在 router 渲染）；
//  2. 交字节（构建器预览 res.HTML、runtime fragment 片段 —— 只搬运 []byte，不拼标签）；
//  3. 出站邮件的正文与追踪像素（SMTP body 属外发内容，不是后台界面）。
//
// 白名单里的条目必须写明「为什么现在还在 Go 里拼」，且**双向判定**：登记了却不再命中同样失败，
// 免得清单退化成注释。存量清零后这张表应当为空。
var htmlLiteralBudget = map[string]int{
	// 出站邮件的内容（SMTP body / 追踪像素）：不是后台界面，前端层管不到它。
	"internal/module/mail/enums/mail_ui_labels.go":  1,
	"internal/module/mail/service/mail_tracking.go": 2,
	// 退订成功页：访客面一次性整页（自带样式、不依赖后台壳）。待迁 internal/templates，
	// 与 admin/preview_notice.html 同一类出口；在此之前先记账。
	"internal/module/mail/inbound/http/mail_page.go": 10,
	// 脚手架生成的是**前端模板文件的内容**（插件目录里的 .html 组件模板），不是后端渲染。
	"internal/module/plugin/scaffold/scaffold.go": 1,
	// 工作台：大纲树 / repeater 行仍由 Go 拼串（31），预览帧往构建产物里插增强脚本（3）。
	// 前者要等 workbench 页面迁 Jet 一起收；后者是对产物做字符串拼接，不是生成结构。
	//
	// 2 → 3 的理由：编辑器桥接脚本已挪成 scripts/editor_bridge.js（go:embed），
	// <script> 包装收进唯一的 editorBridgeScriptTag 一处。此前包装与脚本体混在同一个
	// 常量里，门禁只能看见「2 处」；现在看得见包装点了。**这是记账口径变清楚，
	// 不是新增了拼 HTML 的地方** —— 注入路径要往已渲染的产物字节里插脚本，
	// 走不了 Jet 模板。
	"internal/module/workbench/service/workbench.go":           31,
	// 页面逻辑按域拆成 5 个文件后，三处 HTML 字面量跟着边界点走：
	// 两处（模板文案填充 / 页面渲染失败）在画布出口，一处（<script> 包装）在桥接文件。
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go": 3,
}

var htmlLiteralRe = regexp.MustCompile(
	"<[ \t]*[/]?[ \t]*(?i:doctype|html|head|body|div|span|table|thead|tbody|tr|td|th|ul|ol|li|p|a|h1|h2|h3|h4|h5|h6|form|input|button|select|option|textarea|label|img|script|style|svg|section|header|footer|nav|template|meta|br|hr|strong|em)[ \t\n>/]")

// TestNoHTMLLiteralsInBackend：Go 代码里不得拼 HTML。
func TestNoHTMLLiteralsInBackend(t *testing.T) {
	root := repoRoot(t)
	got := map[string]int{}
	for _, dir := range []string{"internal/module", "internal/routers"} {
		walkHTMLLiterals(t, filepath.Join(root, dir), got)
	}

	for file, want := range htmlLiteralBudget {
		if got[file] != want {
			t.Errorf("%s：登记 %d 处 HTML 字面量，实测 %d 处。拼 HTML 请搬进 internal/templates 的 Jet 模板；"+
				"确实还要留在 Go 里就在 htmlLiteralBudget 写明理由并改数字（清零后删条目）", file, want, got[file])
		}
		delete(got, file)
	}
	if len(got) == 0 {
		return
	}
	files := make([]string, 0, len(got))
	for file := range got {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		t.Errorf("%s：Go 代码里出现 %d 处 HTML 标签 —— 后端不拼 HTML，搬进 internal/templates 的模板", file, got[file])
	}
}

func walkHTMLLiterals(t *testing.T, dir string, got map[string]int) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("解析 %s 失败：%v", path, perr)
		}
		count := 0
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			raw, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				raw = lit.Value
			}
			if htmlLiteralRe.MatchString(raw) {
				count++
			}
			return true
		})
		if count > 0 {
			rel, rerr := filepath.Rel(repoRoot(t), path)
			if rerr != nil {
				rel = path
			}
			got[filepath.ToSlash(rel)] += count
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败：%v", dir, err)
	}
}

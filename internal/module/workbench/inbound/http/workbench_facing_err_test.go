package workbenchhttp

// workbench_facing_err_test.go — 422 可归因文案的分类与「三处一致」的守卫。
//
// 为什么这里要有一条纯逻辑单测：422 的文案来自**三个地方**，任何一处单独改动都不会让编译
// 或其它测试失败 ——
//
//	① workbenchenums 的 key 常量（enums/workbench_enums.go）；
//	② Go 里的中文兜底（workbench_err.go 的 workbenchFacingFallbacks）；
//	③ 迁移 292 的中英词条（public/migrations/292_workbench_facing_i18n.sql）。
//
// 漏配的表现是「英文站点显示中文」或「页面上原样显示 workbench.err.xxx」，前者不会报错、
// 后者要等用户看见，所以在这里按**全量**对账：key 集合相等、zh 兜底与词条逐字相同、
// en 词条非空且不是中文复制。分类判据（文档事实 / 哨兵 key）也一并钉住，避免将来有人
// 把「结构模板带字段绑定」退回成一句泛化的「预览编译失败」。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagecontract "go_wp/internal/module/page/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

// 结构文档夹具：settings 合法，只有 root 不同。
const (
	facingDocPrefix                   = `{"settings":{"layout":{"mode":"full"}},"root":`
	facingStructureHeaderBinding      = facingDocPrefix + `[{"type":"core.heading","id":"h1","props":{"binding":{"field":"post.title"}}}]}`
	facingStructureProductCardBinding = facingDocPrefix + `[{"type":"core.productCard","id":"pc1","props":{"titleField":"item.name"}}]}`
	facingCleanDoc                    = facingDocPrefix + `[{"type":"core.text","id":"t1","props":{"text":"正文"}}]}`
	facingInvalidSettingsDoc          = `{"settings":{"layout":{"mode":"boxed"}},"root":[]}`
)

// TestPreviewCompileFacingKey 预览编译失败的分类（判据是文档事实，不嗅探错误字符串）。
func TestPreviewCompileFacingKey(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		kind    previewDocKind
		wantKey string
		wantOK  bool
	}{
		{
			// 本任务的核心场景：结构模板里带了字段绑定（画布拖入 heading 后填了绑定字段）。
			name: "结构模板含 heading 字段绑定", doc: facingStructureHeaderBinding,
			kind:    previewDocStructureTemplate,
			wantKey: workbenchenums.ErrStructureTemplateFieldBinding, wantOK: true,
		},
		{
			// 自报型组件（FieldBindingProvider）走的是另一条收集路径，必须同样命中。
			name: "结构模板含商品卡字段绑定（自报型组件）", doc: facingStructureProductCardBinding,
			kind:    previewDocStructureTemplate,
			wantKey: workbenchenums.ErrStructureTemplateFieldBinding, wantOK: true,
		},
		{
			name: "页面含字段绑定按页面口径", doc: facingStructureHeaderBinding,
			kind:    previewDocPage,
			wantKey: workbenchenums.ErrPreviewFieldBindingUnsupported, wantOK: true,
		},
		{
			name: "全局块含字段绑定按页面口径", doc: facingStructureHeaderBinding,
			kind:    previewDocBlock,
			wantKey: workbenchenums.ErrPreviewFieldBindingUnsupported, wantOK: true,
		},
		{
			name: "文档没过校验", doc: facingInvalidSettingsDoc,
			kind:    previewDocPage,
			wantKey: workbenchenums.ErrPreviewDocumentInvalid, wantOK: true,
		},
		{
			// 干净文档但编译失败 = 归不了因（装配缺失 / 组件模板加载失败），走归口文案。
			name: "干净文档归口", doc: facingCleanDoc,
			kind: previewDocPage, wantKey: "", wantOK: false,
		},
		{
			name: "非法 JSON 归口", doc: "{",
			kind: previewDocPage, wantKey: "", wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotKey, gotOK := previewCompileFacingKey([]byte(tc.doc), tc.kind)
			if gotOK != tc.wantOK || gotKey != tc.wantKey {
				t.Fatalf("分类不符：got (%q, %v)，want (%q, %v)", gotKey, gotOK, tc.wantKey, tc.wantOK)
			}
		})
	}
}

// TestTemplatePreviewFacingKey 模板预览失败的分类（判据是模块哨兵 key）。
func TestTemplatePreviewFacingKey(t *testing.T) {
	cases := []struct {
		raw     string
		wantKey string
		wantOK  bool
	}{
		{"ErrBuildFailed: ErrFieldBindingInvalid: 字段绑定 product.name 不属于 article 数据源",
			workbenchenums.ErrTemplateFieldBindingInvalid, true},
		{"ErrTemplateTypeMismatch", workbenchenums.ErrTemplateEntityTypeMismatch, true},
		{"ErrProjectRequired", workbenchenums.ErrTemplateProjectScope, true},
		{"ErrProjectNotFound", workbenchenums.ErrTemplateProjectScope, true},
		// 内部错误（装配缺陷）不归因：调用方给归口文案 + 结构化日志。
		{"ErrRegistryMissing", "", false},
		{"实例构建失败: 模板片段解析失败", "", false},
	}
	for _, tc := range cases {
		gotKey, gotOK := templatePreviewFacingKey(tc.raw)
		if gotOK != tc.wantOK || gotKey != tc.wantKey {
			t.Fatalf("%q 分类不符：got (%q, %v)，want (%q, %v)", tc.raw, gotKey, gotOK, tc.wantKey, tc.wantOK)
		}
	}
}

// TestInstanceSaveFacingKey 实例保存失败的分类。
func TestInstanceSaveFacingKey(t *testing.T) {
	cases := []struct {
		raw     string
		wantKey string
		wantOK  bool
	}{
		{"ErrBuildFailed: 编译上下文缺少内容解析器", workbenchenums.ErrInstanceSaveRejected, true},
		{"ErrProjectRequired", workbenchenums.ErrTemplateProjectScope, true},
		{"ErrNotFound", "", false},
	}
	for _, tc := range cases {
		gotKey, gotOK := instanceSaveFacingKey(tc.raw)
		if gotOK != tc.wantOK || gotKey != tc.wantKey {
			t.Fatalf("%q 分类不符：got (%q, %v)，want (%q, %v)", tc.raw, gotKey, gotOK, tc.wantKey, tc.wantOK)
		}
	}
}

// facingRowRe 迁移 292 的词条行：('key', 'lang', '值', ...
//
// 值里刻意不含单引号（迁移 SQL 的语句边界由 SplitStatements 按字面量判定，未配对的引号
// 会让扫描器错位 —— 上一批「多一个分号」整批迁移失败属于同一类事故），所以这个简单正则够用。
var facingRowRe = regexp.MustCompile(`\('(workbench\.err\.[A-Za-z]+)', '(zh-CN|en-US)', '([^']*)',`)

// TestWorkbenchFacingFallbacksMatchMigration292 兜底文案与迁移 292 的词条逐字对账。
func TestWorkbenchFacingFallbacksMatchMigration292(t *testing.T) {
	sqlPath := filepath.Join("..", "..", "..", "..", "..", "public", "migrations", "292_workbench_facing_i18n.sql")
	raw, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("读迁移 292 失败（路径或文件名变了？）：%v", err)
	}
	text := string(raw)

	// 纪律①：文件末尾不写分号（最后一条语句由 SplitStatements 收尾的 appendCurrent 落下）。
	if !strings.HasSuffix(strings.TrimSpace(text), "DO NOTHING") {
		t.Fatalf("迁移 292 应以 ON CONFLICT ... DO NOTHING 结尾且不带分号，实际结尾：%q",
			tailOf(strings.TrimSpace(text), 40))
	}

	zh, en := map[string]string{}, map[string]string{}
	for _, m := range facingRowRe.FindAllStringSubmatch(text, -1) {
		key, lang, value := m[1], m[2], m[3]
		if strings.ContainsAny(value, ";") {
			t.Fatalf("词条 %s/%s 的值含半角分号（会被 SplitStatements 当成语句边界）：%q", key, lang, value)
		}
		switch lang {
		case "zh-CN":
			zh[key] = value
		case "en-US":
			en[key] = value
		}
	}

	// 纪律②：key 集合三处相等 —— 既不能有兜底却没词条，也不能有词条却没兜底（死词条）。
	if len(zh) != len(workbenchFacingFallbacks) {
		t.Fatalf("迁移 292 的 zh-CN key 数 %d 与兜底表 %d 不等", len(zh), len(workbenchFacingFallbacks))
	}
	for key, want := range workbenchFacingFallbacks {
		if got := zh[key]; got != want {
			t.Fatalf("%s 的中文兜底与词条不一致：兜底 %s / 词条 %s", key, want, got)
		}
		if strings.TrimSpace(en[key]) == "" {
			t.Fatalf("%s 缺 en-US 词条（英文站点会回落中文，且不会有任何报错）", key)
		}
		if en[key] == zh[key] {
			t.Fatalf("%s 的 en-US 词条与 zh-CN 相同（多半是复制粘贴漏改）", key)
		}
	}
}

// TestWorkbenchFacingFallbacksCoverExpectedKeys 兜底表就是「可展示文案白名单」：只增不减地钉住。
//
// 与 enums 的对账在 public/test/enums（那边按模块列文件，尚未纳入 workbench）；这里至少保证
// 新增 key 时必须显式回来看一眼这张表，而不是把内部错误顺手放出去。
func TestWorkbenchFacingFallbacksCoverExpectedKeys(t *testing.T) {
	want := []string{
		workbenchenums.ErrStructureTemplateFieldBinding,
		workbenchenums.ErrPreviewFieldBindingUnsupported,
		workbenchenums.ErrPreviewDocumentInvalid,
		workbenchenums.ErrTemplateEntityTypeMismatch,
		workbenchenums.ErrTemplateFieldBindingInvalid,
		workbenchenums.ErrTemplateProjectScope,
		workbenchenums.ErrInstanceNoDocument,
		workbenchenums.ErrInstanceSaveRejected,
	}
	if len(workbenchFacingFallbacks) != len(want) {
		t.Fatalf("兜底表条目数 %d 与本测试的期望 %d 不等（新增 / 删除 key 时同步这里）",
			len(workbenchFacingFallbacks), len(want))
	}
	for _, key := range want {
		if strings.TrimSpace(workbenchFacingFallbacks[key]) == "" {
			t.Fatalf("%s 没有中文兜底（缺词条时用户会看到裸 key）", key)
		}
	}
}

// TestWritePreviewCompileRejectedFacingText 422 出口的响应体：三级判据各走各的出口。
//
// 直接驱动 422 的写出口，断言 ① 状态码仍是 422、② 每一级给出预期的文案、
// ③ **不该出现的文本不出现**（内部原文 / 裸 key）。
// 三级分别对应：类型标记（作者可操作的组件校验提示）、文档事实（字段绑定）、归口（内部错误）。
func TestWritePreviewCompileRejectedFacingText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 模拟 page/service 的包装形状：页面编译失败: <PreviewProblem>（见 page_assemble.go 的
	// builder.Compile 失败分支），外层再由 CompilePreview 包上 ErrPreviewCompileFailed。
	problem := pagecontract.NewPreviewProblem(
		"节点 acc1: 手风琴至少需要一个折叠项（把组件拖入内部）", errors.New("页面编译失败"))
	problemErr := fmt.Errorf("%w: %w", pagecontract.ErrPreviewCompileFailed,
		fmt.Errorf("%w: %w", errors.New("页面编译失败"), problem))

	bindingErr := errors.New("预览编译失败: 页面编译失败: 编译上下文缺少内容解析器，无法解析绑定 \"post.title\"")
	internalErr := errors.New("预览编译失败: 页面编译失败: 节点 nav1: 已绑定导航（位置 \"header\" / 菜单项 \"\"），" +
		"但构建期缺少导航解析器或工程 ID（装配未注入）")

	cases := []struct {
		name     string
		doc      string
		kind     previewDocKind
		err      error
		wantBody string
		// notContains 该场景下**不得**出现的文本。
		notContains []string
	}{
		{
			name: "级别①类型标记：组件校验提示带原文透出", doc: facingCleanDoc, kind: previewDocPage, err: problemErr,
			wantBody:    "预览编译失败：节点 acc1: 手风琴至少需要一个折叠项（把组件拖入内部）",
			notContains: []string{"workbench.err."},
		},
		{
			name: "级别②文档事实：结构模板字段绑定给可归因文案", doc: facingStructureHeaderBinding,
			kind: previewDocStructureTemplate, err: bindingErr,
			wantBody:    workbenchFacingFallbacks[workbenchenums.ErrStructureTemplateFieldBinding],
			notContains: []string{"编译上下文缺少内容解析器", "post.title", "页面编译失败", "workbench.err."},
		},
		{
			name: "级别③归口：内部错误只给归口文案", doc: facingCleanDoc, kind: previewDocPage, err: internalErr,
			wantBody:    workbenchCompileFallback,
			notContains: []string{"装配未注入", "导航解析器", "nav1", "页面编译失败"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/workbench/template/preview", nil)

			writePreviewCompileRejected(c, []byte(tc.doc), tc.kind, tc.err)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("状态码应保持 422，实际 %d", rec.Code)
			}
			body := rec.Body.String()
			if body != tc.wantBody {
				t.Fatalf("响应体不符：got %q / want %q", body, tc.wantBody)
			}
			for _, leak := range tc.notContains {
				if strings.Contains(body, leak) {
					t.Fatalf("响应体不得出现 %q（原文只进结构化日志 / 不得显示裸 key）：%s", leak, body)
				}
			}
		})
	}
}

// tailOf 取字符串末尾 n 个字符（错误信息里展示结尾用）。
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

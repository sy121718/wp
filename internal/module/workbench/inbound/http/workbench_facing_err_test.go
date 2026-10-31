package workbenchhttp

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

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	pagecontract "go_wp/internal/module/page/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
)

const (
	facingDocPrefix                   = `{"settings":{"layout":{"mode":"full"}},"root":`
	facingStructureHeaderBinding      = facingDocPrefix + `[{"type":"core.heading","id":"h1","props":{"binding":{"field":"post.title"}}}]}`
	facingStructureProductCardBinding = facingDocPrefix + `[{"type":"core.productCard","id":"pc1","props":{"titleField":"item.name"}}]}`
	facingCleanDoc                    = facingDocPrefix + `[{"type":"core.text","id":"t1","props":{"text":"正文"}}]}`
	facingCollectionDoc               = facingDocPrefix + `[{"type":"core.productList","id":"pl1","props":{"collectionSource":"content:product","titleField":"item.name"}}]}`
	facingInvalidSettingsDoc          = `{"settings":{"layout":{"mode":"boxed"}},"root":[]}`
)

var workbenchFacingFallbacks = map[string]string{
	workbenchenums.ErrStructureTemplateFieldBinding:  workbenchenums.FallbackFor(workbenchenums.ErrStructureTemplateFieldBinding),
	workbenchenums.ErrPreviewFieldBindingUnsupported: workbenchenums.FallbackFor(workbenchenums.ErrPreviewFieldBindingUnsupported),
	workbenchenums.ErrPreviewDocumentInvalid:         workbenchenums.FallbackFor(workbenchenums.ErrPreviewDocumentInvalid),
	workbenchenums.ErrTemplateEntityTypeMismatch:     workbenchenums.FallbackFor(workbenchenums.ErrTemplateEntityTypeMismatch),
	workbenchenums.ErrTemplateFieldBindingInvalid:    workbenchenums.FallbackFor(workbenchenums.ErrTemplateFieldBindingInvalid),
	workbenchenums.ErrTemplateProjectScope:           workbenchenums.FallbackFor(workbenchenums.ErrTemplateProjectScope),
	workbenchenums.ErrInstanceNoDocument:             workbenchenums.FallbackFor(workbenchenums.ErrInstanceNoDocument),
	workbenchenums.ErrInstanceSaveRejected:           workbenchenums.FallbackFor(workbenchenums.ErrInstanceSaveRejected),
}

func TestPreviewCompileFacingKey(t *testing.T) {
	bindingErr := fmt.Errorf("节点字段解析: %w", core.ErrContentResolverRequired)
	cases := []struct {
		name    string
		doc     string
		kind    workbenchservice.PreviewDocKind
		err     error
		wantKey string
		wantOK  bool
	}{
		{

			name: "结构模板含 heading 字段绑定", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocStructureTemplate, err: bindingErr,
			wantKey: workbenchenums.ErrStructureTemplateFieldBinding, wantOK: true,
		},
		{

			name: "结构模板含商品卡字段绑定（自报型组件）", doc: facingStructureProductCardBinding,
			kind: workbenchservice.PreviewDocStructureTemplate, err: bindingErr,
			wantKey: workbenchenums.ErrStructureTemplateFieldBinding, wantOK: true,
		},
		{
			name: "页面含字段绑定按页面口径", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocPage, err: bindingErr,
			wantKey: workbenchenums.ErrPreviewFieldBindingUnsupported, wantOK: true,
		},
		{
			name: "全局块含字段绑定按页面口径", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocBlock, err: bindingErr,
			wantKey: workbenchenums.ErrPreviewFieldBindingUnsupported, wantOK: true,
		},
		{
			name: "文档没过校验", doc: facingInvalidSettingsDoc,
			kind:    workbenchservice.PreviewDocPage,
			wantKey: workbenchenums.ErrPreviewDocumentInvalid, wantOK: true,
		},
		{

			name: "干净文档归口", doc: facingCleanDoc,
			kind: workbenchservice.PreviewDocPage, wantKey: "", wantOK: false,
		},
		{
			name: "合法集合项绑定不覆盖其它编译错误", doc: facingCollectionDoc,
			kind: workbenchservice.PreviewDocPage, err: errors.New("内部动作解析失败"),
			wantKey: "", wantOK: false,
		},
		{
			name: "失效发布目标按真实错误链提示重选", doc: facingCollectionDoc,
			kind: workbenchservice.PreviewDocPage, err: fmt.Errorf("%w: /private/active/path", builder.ErrPublishedActionUnavailable),
			wantKey: workbenchenums.ErrPreviewActionTargetUnavailable, wantOK: true,
		},
		{
			name: "相同错误文本不能冒充错误事实", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocPage, err: errors.New("编译上下文缺少内容解析器"),
			wantKey: "", wantOK: false,
		},
		{
			name: "非法 JSON 归口", doc: "{",
			kind: workbenchservice.PreviewDocPage, wantKey: "", wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotKey, gotOK := workbenchservice.PreviewCompileFacingKey([]byte(tc.doc), tc.kind, tc.err)
			if gotOK != tc.wantOK || gotKey != tc.wantKey {
				t.Fatalf("分类不符：got (%q, %v)，want (%q, %v)", gotKey, gotOK, tc.wantKey, tc.wantOK)
			}
		})
	}
}

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

		{"ErrRegistryMissing", "", false},
		{"实例构建失败: 模板片段解析失败", "", false},
	}
	for _, tc := range cases {
		gotKey, gotOK := workbenchservice.TemplatePreviewFacingKey(tc.raw)
		if gotOK != tc.wantOK || gotKey != tc.wantKey {
			t.Fatalf("%q 分类不符：got (%q, %v)，want (%q, %v)", tc.raw, gotKey, gotOK, tc.wantKey, tc.wantOK)
		}
	}
}

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
		gotKey, gotOK := workbenchservice.InstanceSaveFacingKey(tc.raw)
		if gotOK != tc.wantOK || gotKey != tc.wantKey {
			t.Fatalf("%q 分类不符：got (%q, %v)，want (%q, %v)", tc.raw, gotKey, gotOK, tc.wantKey, tc.wantOK)
		}
	}
}

var facingRowRe = regexp.MustCompile(`\('(workbench\.err\.[A-Za-z]+)', '(zh-CN|en-US)', '([^']*)',`)

func TestWorkbenchFacingFallbacksMatchMigration292(t *testing.T) {
	sqlPath := filepath.Join("..", "..", "..", "..", "..", "public", "migrations", "292_workbench_facing_i18n.sql")
	raw, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("读迁移 292 失败（路径或文件名变了？）：%v", err)
	}
	text := string(raw)

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

func TestWritePreviewCompileRejectedFacingText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	problem := pagecontract.NewPreviewProblem(
		"节点 acc1: 手风琴至少需要一个折叠项（把组件拖入内部）", errors.New("页面编译失败"))
	problemErr := fmt.Errorf("%w: %w", pagecontract.ErrPreviewCompileFailed,
		fmt.Errorf("%w: %w", errors.New("页面编译失败"), problem))

	bindingErr := fmt.Errorf("预览编译失败: 页面编译失败: %w，无法解析绑定 %q", core.ErrContentResolverRequired, "post.title")
	internalErr := errors.New("预览编译失败: 页面编译失败: 节点 nav1: 已绑定导航（位置 \"header\" / 菜单项 \"\"），" +
		"但构建期缺少导航解析器或工程 ID（装配未注入）")

	cases := []struct {
		name     string
		doc      string
		kind     workbenchservice.PreviewDocKind
		err      error
		wantBody string

		notContains []string
	}{
		{
			name: "级别①类型标记：组件校验提示带原文透出", doc: facingCleanDoc, kind: workbenchservice.PreviewDocPage, err: problemErr,
			wantBody:    "预览编译失败：节点 acc1: 手风琴至少需要一个折叠项（把组件拖入内部）",
			notContains: []string{"workbench.err."},
		},
		{
			name: "级别②错误链事实：结构模板字段绑定给可归因文案", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocStructureTemplate, err: bindingErr,
			wantBody:    workbenchFacingFallbacks[workbenchenums.ErrStructureTemplateFieldBinding],
			notContains: []string{"编译上下文缺少内容解析器", "post.title", "页面编译失败", "workbench.err."},
		},
		{
			name: "级别②错误链事实：普通页面仍拒绝实体字段绑定", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocPage, err: bindingErr,
			wantBody:    workbenchFacingFallbacks[workbenchenums.ErrPreviewFieldBindingUnsupported],
			notContains: []string{"编译上下文缺少内容解析器", "post.title", "页面编译失败", "workbench.err."},
		},
		{
			name: "失效跳转目标的422只给安全重选提示", doc: facingCollectionDoc, kind: workbenchservice.PreviewDocPage,
			err:         fmt.Errorf("%w: 路径 /private/active/chosen 已被非符号链接占位", builder.ErrPublishedActionUnavailable),
			wantBody:    workbenchenums.FallbackFor(workbenchenums.ErrPreviewActionTargetUnavailable),
			notContains: []string{"/private/active", "非符号链接", "workbench.err.", "字段绑定"},
		},
		{
			name: "级别③归口：合法集合项不能把内部错误改成字段上下文错误", doc: facingCollectionDoc,
			kind: workbenchservice.PreviewDocPage, err: internalErr,
			wantBody:    workbenchCompileFallback,
			notContains: []string{"装配未注入", "导航解析器", "nav1", "页面编译失败", "字段绑定", "workbench.err."},
		},
		{
			name: "级别③归口：内部错误只给归口文案", doc: facingCleanDoc, kind: workbenchservice.PreviewDocPage, err: internalErr,
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

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

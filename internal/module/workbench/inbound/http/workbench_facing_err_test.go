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

	json "go_wp/pkg/jsonx"

	"github.com/gin-gonic/gin"

	pagecontract "go_wp/internal/module/page/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
)

const (
	facingDocPrefix                   = `{"settings":{"layout":{"mode":"full"}},"root":`
	facingStructureHeaderBinding      = facingDocPrefix + `[{"type":"core.heading","id":"h1","props":{"binding":{"field":"post.title"}}}]}`
	facingStructureProductCardBinding = facingDocPrefix + `[{"type":"core.productCard","id":"pc1","props":{"titleField":"item.name"}}]}`
	facingCleanDoc                    = facingDocPrefix + `[{"type":"core.text","id":"t1","props":{"text":"正文"}}]}`
	facingCollectionDoc               = facingDocPrefix + `[{"type":"core.productList","id":"pl1","props":{"collectionSource":"content:product","titleField":"item.name"}}]}`
	facingInvalidSettingsDoc          = `{"settings":{"layout":{"mode":"boxed"}},"root":[]}`
)

// workbenchFacingKeys 是本测试关心的错误 key 清单（顺序即词条应覆盖的顺序）。
// enums 只声明 key + 状态码，成品句归词库；期望文案从 public/schema 的系统数据现读。
var workbenchFacingKeys = []string{
	workbenchenums.ErrStructureTemplateFieldBinding,
	workbenchenums.ErrPreviewFieldBindingUnsupported,
	workbenchenums.ErrPreviewDocumentInvalid,
	workbenchenums.ErrTemplateEntityTypeMismatch,
	workbenchenums.ErrTemplateFieldBindingInvalid,
	workbenchenums.ErrTemplateProjectScope,
	workbenchenums.ErrInstanceNoDocument,
	workbenchenums.ErrInstanceSaveRejected,
}

// seededFacingText 取某个 key 的 zh-CN 译文，作为对外文案的期望值。
func seededFacingText(t *testing.T, key string) string {
	t.Helper()
	for _, m := range facingRowRe.FindAllStringSubmatch(readWorkbenchFacingSeed(t), -1) {
		if m[1] == key && m[2] == "zh-CN" {
			return m[3]
		}
	}
	t.Fatalf("%s 的 zh-CN 词条不在 public/schema 的系统数据里", key)
	return ""
}

// readWorkbenchFacingSeed 读系统词条真源。
func readWorkbenchFacingSeed(t *testing.T) string {
	t.Helper()
	sqlPath := filepath.Join("..", "..", "..", "..", "..", "public", "schema", "tables", "sys_i18n.sql")
	raw, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatalf("读系统词条失败（路径变了？）：%v", err)
	}
	return string(raw)
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

var facingRowRe = regexp.MustCompile(`\(\s*\d+,\s*'(workbench\.err\.[A-Za-z]+)',\s*'(zh-CN|en-US)',\s*'([^']*)'`)

func TestWorkbenchFacingFallbacksMatchSchema(t *testing.T) {
	text := readWorkbenchFacingSeed(t)
	wanted := map[string]bool{}
	for _, key := range workbenchFacingKeys {
		wanted[key] = true
	}

	zh, en := map[string]string{}, map[string]string{}
	for _, m := range facingRowRe.FindAllStringSubmatch(text, -1) {
		key, lang, value := m[1], m[2], m[3]
		if !wanted[key] {
			continue
		}
		switch lang {
		case "zh-CN":
			zh[key] = value
		case "en-US":
			en[key] = value
		}
	}

	for _, key := range workbenchFacingKeys {
		if strings.TrimSpace(zh[key]) == "" {
			t.Fatalf("%s 在 public/schema 里没有 zh-CN 词条（缺词条时用户会看到裸 key）", key)
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
	text := readWorkbenchFacingSeed(t)
	seeded := map[string]bool{}
	for _, m := range facingRowRe.FindAllStringSubmatch(text, -1) {
		if m[2] == "zh-CN" {
			seeded[m[1]] = true
		}
	}
	for _, key := range workbenchFacingKeys {
		if !seeded[key] {
			t.Fatalf("%s 在 public/schema 里没有 zh-CN 词条（缺词条时用户会看到裸 key）", key)
		}
	}
}

func TestWritePreviewCompileRejectedFacingText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 级别②的子例断的是「seed 里的那句原文」。取词收口后 enums 只声明 key，
	// workbenchFacingText 走 response.MsgText，缺词就退归口文案 —— 不注入词库
	// 测到的就是「系统内部错误」，而不是本来的业务事实。期望值由 seededFacingText
	// 从 seed 现读，这里注入的是同一批 key 的 zh-CN 原文。
	i18n.InjectForTest(map[string]map[string]string{
		workbenchenums.ErrStructureTemplateFieldBinding:  {"zh-CN": seededFacingText(t, workbenchenums.ErrStructureTemplateFieldBinding)},
		workbenchenums.ErrPreviewFieldBindingUnsupported: {"zh-CN": seededFacingText(t, workbenchenums.ErrPreviewFieldBindingUnsupported)},
		workbenchenums.ErrPreviewActionTargetUnavailable: {"zh-CN": seededFacingText(t, workbenchenums.ErrPreviewActionTargetUnavailable)},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

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
			wantBody:    seededFacingText(t, workbenchenums.ErrStructureTemplateFieldBinding),
			notContains: []string{"编译上下文缺少内容解析器", "post.title", "页面编译失败", "workbench.err."},
		},
		{
			name: "级别②错误链事实：普通页面仍拒绝实体字段绑定", doc: facingStructureHeaderBinding,
			kind: workbenchservice.PreviewDocPage, err: bindingErr,
			wantBody:    seededFacingText(t, workbenchenums.ErrPreviewFieldBindingUnsupported),
			notContains: []string{"编译上下文缺少内容解析器", "post.title", "页面编译失败", "workbench.err."},
		},
		{
			name: "失效跳转目标的422只给安全重选提示", doc: facingCollectionDoc, kind: workbenchservice.PreviewDocPage,
			err:         fmt.Errorf("%w: 路径 /private/active/chosen 已被非符号链接占位", builder.ErrPublishedActionUnavailable),
			wantBody:    seededFacingText(t, workbenchenums.ErrPreviewActionTargetUnavailable),
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
			var payload struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if uerr := json.Unmarshal(rec.Body.Bytes(), &payload); uerr != nil {
				t.Fatalf("失败响应应为 JSON：%v，实际 %q", uerr, rec.Body.String())
			}
			if payload.Code != http.StatusUnprocessableEntity {
				t.Fatalf("响应 code 应为 422，实际 %d", payload.Code)
			}
			body := payload.Message
			if body != tc.wantBody {
				t.Fatalf("message 不符：got %q / want %q", body, tc.wantBody)
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

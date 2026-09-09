package unit

// content_validate_test.go — 工作台写入校验与构建期候选同源（多语言 P5c，docs/06-D §7.8）。
//
// 目标：证明「工作台能写什么」与「构建期会取什么」是同一份判据，而不是两套扫描：
//  1. 工作台列出的行来自 builder.CollectContentCandidates（P5b 唯一候选来源），
//     本用例对同一份文档逐条断言「候选 → 可写入」（白名单 + 长度 + 形态全过）；
//  2. 未声明字段 / 未注册类型 → 一律拒绝（构建期永不取用的字段不许进翻译表）；
//  3. 长度上限取「控件 maxlen」与 core.MaxRichLen 的较小值（富文本超 30000 判空）；
//  4. 形态一致：原文含 HTML 标签时译文也必须含标签，反之亦然。

import (
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// p5cDoc 覆盖多种组件与字段形态的页面文档（纯解析，不做完整校验）。
const p5cDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "工作台", "description": "P5c"}},
  "root": [
    {"id": "hd1", "type": "core.heading", "props": {"text": "关于我们", "subtitle": "了解更多"}},
    {"id": "tx1", "type": "core.text", "props": {"text": "<p>我们成立于 2010 年</p>"}},
    {"id": "bt1", "type": "core.button", "props": {"text": "联系我们", "link": "/contact"}},
    {"id": "cd1", "type": "core.card", "props": {"title": "卡片标题", "text": "<p>卡片正文</p>", "buttonText": "查看详情"}},
    {"id": "im1", "type": "core.image", "props": {"src": "/storage/image/a.jpg", "alt": "公司前台", "caption": "前台照片"}},
    {"id": "tb1", "type": "core.table", "props": {"caption": "季度数据", "headers": ["名称", "金额"], "rows": [["收入", "42"]]}},
    {"id": "tx2", "type": "core.text", "props": {"text": "2024"}}
  ]
}`

// TestWorkbenchCandidatesSameSourceAsBuild 工作台清单 = 构建期候选（同一份来源，逐条可写）。
func TestWorkbenchCandidatesSameSourceAsBuild(t *testing.T) {
	page := parse(t, p5cDoc)
	candidates := builder.CollectContentCandidates(page)
	if len(candidates) == 0 {
		t.Fatal("候选为空：工作台将无行可编辑")
	}

	seen := map[string]bool{}
	for _, c := range candidates {
		typ, field, ok := i18n.ParseContentContext(c.Context)
		if !ok {
			t.Fatalf("候选语境非法: %q", c.Context)
		}
		if !core.TranslatableFields(typ)[field] {
			t.Fatalf("候选 %s 不在组件白名单内（收集与白名单漂移）", c.Context)
		}
		// 同一份候选直接作为工作台输入：原文即译文时也必须通过写入校验。
		if err := builder.ValidateContentTarget(c.Context, c.Source, c.Source); err != nil {
			t.Fatalf("候选 %s（原文 %q）无法写入: %v", c.Context, c.Source, err)
		}
		seen[c.Context+"\x00"+c.Source] = true
	}

	// 跳过规则命中的取值不进候选（纯数字 2024 的 core.text.text 被丢弃）。
	for _, c := range candidates {
		if c.Context == "core.text.text" && c.Source == "2024" {
			t.Fatal("纯数字取值不应出现在候选里（跳过规则，§7.6）")
		}
	}
	// 未声明字段不进候选（button.link / image.src）。
	for _, bad := range []string{"core.button.link", "core.image.src", "core.heading.level"} {
		for _, c := range candidates {
			if c.Context == bad {
				t.Fatalf("未声明字段 %s 不应进入候选", bad)
			}
		}
	}
	// 同一文本出现在不同语境 → 两行（各自成候选）。
	if !seen["core.heading.subtitle\x00了解更多"] || !seen["core.button.text\x00联系我们"] {
		t.Fatalf("候选去重/语境切分异常: %+v", candidates)
	}
}

// TestValidateContentTargetRejectsNonWhitelisted 未声明字段 / 未注册类型一律拒绝。
func TestValidateContentTargetRejectsNonWhitelisted(t *testing.T) {
	for _, tc := range []struct{ context, source string }{
		{"core.button.link", "/shop"},
		{"core.image.src", "/storage/image/a.jpg"},
		{"core.heading.level", "h2"},
		{"core.notexist.text", "hello"},
		{"core.container.tag", "section"},
		{"notacomponent", "hello"},
		{"core.button", "hello"}, // 缺少字段名
	} {
		err := builder.ValidateContentTarget(tc.context, tc.source, "译文")
		if !errors.Is(err, builder.ErrContentContextInvalid) {
			t.Fatalf("%s 应被拒为语境非法，实际: %v", tc.context, err)
		}
	}
}

// TestContentTargetLimit 上限取「控件 maxlen」与 MaxRichLen 的较小值。
func TestContentTargetLimit(t *testing.T) {
	for _, tc := range []struct {
		context string
		want    int
	}{
		{"core.button.text", 200},     // 控件 maxlen=200
		{"core.heading.text", 500},    // 控件 maxlen=500
		{"core.card.text", 1000},      // richtext + maxlen=1000
		{"core.text.text", 30000},     // richtext 上限 = MaxRichLen
		{"core.table.headers", 30000}, // 嵌套数组字段无控件声明 → MaxRichLen 兜底
	} {
		limit, ok := builder.ContentTargetLimit(tc.context)
		if !ok {
			t.Fatalf("%s 应有上限（白名单字段）", tc.context)
		}
		if limit != tc.want {
			t.Fatalf("%s 上限应为 %d，实际 %d", tc.context, tc.want, limit)
		}
	}
	if _, ok := builder.ContentTargetLimit("core.button.link"); ok {
		t.Fatal("未声明字段不应有上限")
	}
}

// TestValidateContentTargetLength 超长译文拒绝，边界值放行。
func TestValidateContentTargetLength(t *testing.T) {
	atLimit := strings.Repeat("a", 200)
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", atLimit); err != nil {
		t.Fatalf("等于上限（200 字节）应通过: %v", err)
	}
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", atLimit+"a"); !errors.Is(err, builder.ErrContentTargetTooLong) {
		t.Fatalf("超上限 1 字节应被拒，实际: %v", err)
	}

	// 富文本超 MaxRichLen（30000）→ 构建期判空，必须拒写。
	richSource := "<p>" + strings.Repeat("x", 100) + "</p>"
	huge := "<p>" + strings.Repeat("y", core.MaxRichLen) + "</p>"
	if err := builder.ValidateContentTarget("core.text.text", richSource, huge); !errors.Is(err, builder.ErrContentTargetTooLong) {
		t.Fatalf("富文本超 MaxRichLen 应被拒，实际: %v", err)
	}
	// 嵌套字段（无控件 maxlen）同样受 MaxRichLen 兜底。
	hugePlain := strings.Repeat("z", core.MaxRichLen+1)
	if err := builder.ValidateContentTarget("core.table.headers", "名称", hugePlain); !errors.Is(err, builder.ErrContentTargetTooLong) {
		t.Fatalf("嵌套字段超 MaxRichLen 应被拒，实际: %v", err)
	}
}

// TestValidateContentTargetShape 形态一致：HTML 与纯文本不可混用。
func TestValidateContentTargetShape(t *testing.T) {
	// 原文富文本 → 译文必须含标签。
	if err := builder.ValidateContentTarget("core.text.text", "<p>我们成立于 2010 年</p>", "<p>Founded in 2010</p>"); err != nil {
		t.Fatalf("富文本 → 富文本应通过: %v", err)
	}
	if err := builder.ValidateContentTarget("core.text.text", "<p>我们成立于 2010 年</p>", "Founded in 2010"); !errors.Is(err, builder.ErrContentTargetShape) {
		t.Fatalf("富文本 → 纯文本应被拒（会被归一为段落化纯文本），实际: %v", err)
	}
	// 原文纯文本 → 译文不得含标签。
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", "Learn more"); err != nil {
		t.Fatalf("纯文本 → 纯文本应通过: %v", err)
	}
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", "<strong>Learn more</strong>"); !errors.Is(err, builder.ErrContentTargetShape) {
		t.Fatalf("纯文本 → 含标签应被拒，实际: %v", err)
	}
}

// TestValidateContentTargetRejectsSkippedSource 跳过规则的原文不允许写译文。
func TestValidateContentTargetRejectsSkippedSource(t *testing.T) {
	for _, source := range []string{"", "   ", "2024", "-5", "3.14", "→", "--"} {
		if err := builder.ValidateContentTarget("core.text.text", source, "target"); !errors.Is(err, builder.ErrContentSourceSkipped) {
			t.Fatalf("原文 %q 应被拒为「不参与翻译」，实际: %v", source, err)
		}
	}
	if err := builder.ValidateContentTarget("core.text.text", "共 42 件", "  "); !errors.Is(err, builder.ErrContentTargetEmpty) {
		t.Fatalf("空白译文应被拒为空，实际: %v", err)
	}
}

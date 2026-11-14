package unit

import (
	"errors"
	"strings"
	"testing"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
	"go_wp/pkg/richtext"
)

const p5cDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "工作台", "description": "P5c"}},
  "root": [
    {"id": "hd1", "type": "core.heading", "props": {"text": "关于我们", "subtitle": "了解更多"}},
    {"id": "tx1", "type": "core.text", "props": {"text": "<p>我们成立于 2010 年</p>"}},
    {"id": "bt1", "type": "core.button", "props": {"text": "联系我们", "link": "/contact"}},
    {"id": "cd1", "type": "core.card", "props": {"title": "卡片标题", "text": "<p>卡片正文</p>", "label": "查看详情"}},
    {"id": "im1", "type": "core.image", "props": {"src": "/storage/image/a.jpg", "alt": "公司前台", "caption": "前台照片"}},
    {"id": "tb1", "type": "core.table", "props": {"caption": "季度数据", "headers": ["名称", "金额"], "rows": [["收入", "42"]]}},
    {"id": "tx2", "type": "core.text", "props": {"text": "2024"}}
  ]
}`

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

		if err := builder.ValidateContentTarget(c.Context, c.Source, c.Source); err != nil {
			t.Fatalf("候选 %s（原文 %q）无法写入: %v", c.Context, c.Source, err)
		}
		seen[c.Context+"\x00"+c.Source] = true
	}

	for _, c := range candidates {
		if c.Context == "core.text.text" && c.Source == "2024" {
			t.Fatal("纯数字取值不应出现在候选里（跳过规则，§7.6）")
		}
	}

	for _, bad := range []string{"core.button.link", "core.image.src", "core.heading.level"} {
		for _, c := range candidates {
			if c.Context == bad {
				t.Fatalf("未声明字段 %s 不应进入候选", bad)
			}
		}
	}

	if !seen["core.heading.subtitle\x00了解更多"] || !seen["core.button.text\x00联系我们"] {
		t.Fatalf("候选去重/语境切分异常: %+v", candidates)
	}
}

func TestValidateContentTargetRejectsNonWhitelisted(t *testing.T) {
	for _, tc := range []struct{ context, source string }{
		{"core.button.link", "/shop"},
		{"core.image.src", "/storage/image/a.jpg"},
		{"core.heading.level", "h2"},
		{"core.notexist.text", "hello"},
		{"core.container.tag", "section"},
		{"notacomponent", "hello"},
		{"core.button", "hello"},
	} {
		err := builder.ValidateContentTarget(tc.context, tc.source, "译文")
		if !errors.Is(err, builder.ErrContentContextInvalid) {
			t.Fatalf("%s 应被拒为语境非法，实际: %v", tc.context, err)
		}
	}
}

func TestContentTargetLimit(t *testing.T) {
	for _, tc := range []struct {
		context string
		want    int
	}{
		{"core.button.text", 200},
		{"core.heading.text", 500},
		{"core.card.text", 1000},
		{"core.text.text", 30000},
		{"core.table.headers", 30000},
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

func TestValidateContentTargetLength(t *testing.T) {
	atLimit := strings.Repeat("a", 200)
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", atLimit); err != nil {
		t.Fatalf("等于上限（200 字节）应通过: %v", err)
	}
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", atLimit+"a"); !errors.Is(err, builder.ErrContentTargetTooLong) {
		t.Fatalf("超上限 1 字节应被拒，实际: %v", err)
	}

	richSource := "<p>" + strings.Repeat("x", 100) + "</p>"
	huge := "<p>" + strings.Repeat("y", richtext.MaxRichLen) + "</p>"
	if err := builder.ValidateContentTarget("core.text.text", richSource, huge); !errors.Is(err, builder.ErrContentTargetTooLong) {
		t.Fatalf("富文本超 MaxRichLen 应被拒，实际: %v", err)
	}

	hugePlain := strings.Repeat("z", richtext.MaxRichLen+1)
	if err := builder.ValidateContentTarget("core.table.headers", "名称", hugePlain); !errors.Is(err, builder.ErrContentTargetTooLong) {
		t.Fatalf("嵌套字段超 MaxRichLen 应被拒，实际: %v", err)
	}
}

func TestValidateContentTargetShape(t *testing.T) {

	if err := builder.ValidateContentTarget("core.text.text", "<p>我们成立于 2010 年</p>", "<p>Founded in 2010</p>"); err != nil {
		t.Fatalf("富文本 → 富文本应通过: %v", err)
	}
	if err := builder.ValidateContentTarget("core.text.text", "<p>我们成立于 2010 年</p>", "Founded in 2010"); !errors.Is(err, builder.ErrContentTargetShape) {
		t.Fatalf("富文本 → 纯文本应被拒（会被归一为段落化纯文本），实际: %v", err)
	}

	if err := builder.ValidateContentTarget("core.button.text", "了解更多", "Learn more"); err != nil {
		t.Fatalf("纯文本 → 纯文本应通过: %v", err)
	}
	if err := builder.ValidateContentTarget("core.button.text", "了解更多", "<strong>Learn more</strong>"); !errors.Is(err, builder.ErrContentTargetShape) {
		t.Fatalf("纯文本 → 含标签应被拒，实际: %v", err)
	}
}

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

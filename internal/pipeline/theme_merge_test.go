package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"go_wp/internal/builder"
)

func TestMergeActiveThemeIntoDocument_NoProject(t *testing.T) {
	doc := json.RawMessage(`{"settings":{"structure":{"headerBlockId":"h1"}},"root":{}}`)
	out, err := MergeActiveThemeIntoDocument(context.Background(), nil, "p1", doc)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Settings struct {
			Theme     json.RawMessage `json:"theme"`
			Structure struct {
				Header string `json:"headerBlockId"`
			} `json:"structure"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Settings.Structure.Header != "h1" {
		t.Fatalf("应保留页面 structure，实际 %q", parsed.Settings.Structure.Header)
	}
}

func TestParseStructureBindings(t *testing.T) {
	b, err := ParseStructureBindings(json.RawMessage(`{"settings":{"structure":{"headerBlockId":"a","footerBlockId":"b"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.HeaderBlockID != "a" || b.FooterBlockID != "b" {
		t.Fatalf("unexpected %+v", b)
	}
}

func TestMergeStructureBindingsPageOverridePreserved(t *testing.T) {
	page := builder.StructureBindings{
		HeaderBlockID: "page-header",
		FooterBlockID: "page-footer",
	}
	theme := builder.StructureBindings{
		HeaderBlockID: "theme-header",
		FooterBlockID: "theme-footer",
	}
	got := MergeStructureBindings(page, theme)
	if got.HeaderBlockID != "page-header" {
		t.Fatalf("页眉应保留页面绑定，实际 %q", got.HeaderBlockID)
	}
	if got.FooterBlockID != "page-footer" {
		t.Fatalf("页脚应保留页面绑定，实际 %q", got.FooterBlockID)
	}

	// 页面只覆盖一侧时，空侧回落主题默认。
	partial := builder.StructureBindings{HeaderBlockID: "page-only-header"}
	got = MergeStructureBindings(partial, theme)
	if got.HeaderBlockID != "page-only-header" || got.FooterBlockID != "theme-footer" {
		t.Fatalf("空字段应回落主题默认，实际 %+v", got)
	}
}

package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
)

// TestBlockKindNormalization 通过 Create 落库 kind 间接覆盖私有 normalizeKind：
// 空值默认 block；header/footer 精确识别（TrimSpace 后）；未知值回退 block。
func TestBlockKindNormalization(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"EmptyDefaultsBlock", "", blockmodel.KindBlock},
		{"UnknownFallsBackToBlock", "hero", blockmodel.KindBlock},
		{"WhitespaceOnlyDefaultsBlock", "   ", blockmodel.KindBlock},
		{"HeaderExact", "header", blockmodel.KindHeader},
		{"FooterExact", "footer", blockmodel.KindFooter},
		{"HeaderWithWhitespace", "  header  ", blockmodel.KindHeader},
		{"FooterWithTabNewline", "\tfooter\n", blockmodel.KindFooter},
		// 观察点：normalizeKind 大小写敏感（只 TrimSpace，不做 EqualFold）：
		// 大写/混合大小写不被识别，静默回退 block。
		{"UpperHeaderFallsBackToBlock", "HEADER", blockmodel.KindBlock},
		{"MixedCaseFooterFallsBackToBlock", "Footer", blockmodel.KindBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := e.svc.Create(ctx, &blockdto.CreateReq{
				ProjectID: e.projectID, Name: "kind-" + tc.name, Kind: tc.raw,
			})
			if err != nil {
				t.Fatalf("创建应成功: %v", err)
			}
			if res.Kind != tc.want {
				t.Fatalf("normalizeKind(%q) 落库为 %q，期望 %q", tc.raw, res.Kind, tc.want)
			}
		})
	}
}

// TestBlockDocumentValidation 通过 Create 间接覆盖私有 validateDocument：
// 合法文档规范化入库（默认文档、显式文档）；非法/恶意 JSON 一律拒绝。
func TestBlockDocumentValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("ValidDocuments", func(t *testing.T) {
		valid := []struct {
			name string
			doc  string
		}{
			{"EmptyUsesDefaultDocument", ""},
			{"FullLayoutEmptyRoot", `{"settings":{"layout":{"mode":"full"}},"root":[]}`},
			{"WithContainerNode", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}`},
			{"BoxedWithMaxWidth", `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[]}`},
		}
		for _, tc := range valid {
			t.Run(tc.name, func(t *testing.T) {
				res, err := e.svc.Create(ctx, &blockdto.CreateReq{
					ProjectID: e.projectID, Name: "doc-" + tc.name, Document: json.RawMessage(tc.doc),
				})
				if err != nil {
					t.Fatalf("合法文档应通过: %v", err)
				}
				if got := docLayoutMode(t, res.Document); got != "full" && got != "boxed" {
					t.Fatalf("规范化文档 layout.mode 异常: %q", got)
				}
			})
		}
	})

	t.Run("InvalidDocuments", func(t *testing.T) {
		invalid := []struct {
			name string
			doc  string
		}{
			{"BrokenJSON", `{not-json`},
			{"NullDocument", `null`},
			{"JSONArray", `[]`},
			{"JSONString", `"hello"`},
			{"JSONNumber", `123`},
			{"EmptyObject", `{}`},
			{"InvalidLayoutMode", `{"settings":{"layout":{"mode":"weird"}},"root":[]}`},
			{"BoxedWithoutMaxWidth", `{"settings":{"layout":{"mode":"boxed"}},"root":[]}`},
			{"UnknownComponentType", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.unknown","props":{}}]}`},
			{"DuplicateNodeID", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}},{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}]}`},
			{"InvalidTag", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"script"}}]}`},
			{"CSSInjectionInProps", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","box":{"padding":"1px;background:url(javascript:alert(1))"}}}]}`},
			{"OversizedTitle", `{"settings":{"layout":{"mode":"full"},"seo":{"title":"` + strings.Repeat("a", 201) + `"}},"root":[]}`},
		}
		for _, tc := range invalid {
			t.Run(tc.name, func(t *testing.T) {
				res, err := e.svc.Create(ctx, &blockdto.CreateReq{
					ProjectID: e.projectID, Name: "bad-" + tc.name, Document: json.RawMessage(tc.doc),
				})
				errContains(t, err, blockenums.ErrBlockInvalidDoc)
				if res != nil {
					t.Fatalf("非法文档不应返回结果: %#v", res)
				}
			})
		}
	})
}

// TestBlockDocumentValidationOnUpdate Update 路径的文档校验：非法文档拒绝且数据不变。
func TestBlockDocumentValidationOnUpdate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	created := e.createBlock(t, "更新文档校验", "")

	cases := []struct {
		name string
		doc  string
	}{
		{"ArrayRejected", `[]`},
		{"NullRejected", `null`},
		{"BrokenJSONRejected", `{"root":[`},
		{"InvalidModeRejected", `{"settings":{"layout":{"mode":"weird"}},"root":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.svc.Update(ctx, &blockdto.UpdateReq{
				ID: created.ID, Name: "不应生效-" + tc.name, Document: json.RawMessage(tc.doc),
			})
			errContains(t, err, blockenums.ErrBlockInvalidDoc)
			got, err := e.svc.Detail(ctx, &blockdto.DetailReq{ID: created.ID})
			if err != nil {
				t.Fatalf("回查失败: %v", err)
			}
			if got.Name != "更新文档校验" {
				t.Fatalf("非法文档更新后名称不应变化: %q", got.Name)
			}
		})
	}
}

// TestBlockEdgeInputs 边界与恶意输入：超长名称/文档、注入字符、Unicode。
func TestBlockEdgeInputs(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	t.Run("VeryLongNameAccepted", func(t *testing.T) {
		// 观察点：service 层不校验名称长度（DTO binding max=100 只作用 HTTP 层），
		// 超长名称可直接入库。
		long := strings.Repeat("名", 2000)
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: long})
		if err != nil {
			t.Fatalf("超长名称创建: %v", err)
		}
		if res.Name != long {
			t.Fatalf("超长名称未完整保存: got %d runes", len([]rune(res.Name)))
		}
	})

	t.Run("UnicodeNameAccepted", func(t *testing.T) {
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "导航块-🚀-中文"})
		if err != nil {
			t.Fatalf("Unicode 名称创建: %v", err)
		}
		if res.Name != "导航块-🚀-中文" {
			t.Fatalf("Unicode 名称异常: %q", res.Name)
		}
	})

	t.Run("NameWithInjectionCharactersAccepted", func(t *testing.T) {
		// 观察点：名称不做字符过滤（存储层职责之外），注入字符按原文保存。
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{ProjectID: e.projectID, Name: "<script>alert(1)</script>"})
		if err != nil {
			t.Fatalf("含注入字符名称创建: %v", err)
		}
		if res.Name != "<script>alert(1)</script>" {
			t.Fatalf("名称被改写: %q", res.Name)
		}
	})

	t.Run("LargeDocumentAccepted", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString(`{"settings":{"layout":{"mode":"full"}},"root":[`)
		for i := 0; i < 200; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, `{"id":"n%d","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}}}`, i)
		}
		sb.WriteString(`]}`)
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{
			ProjectID: e.projectID, Name: "大文档", Document: json.RawMessage(sb.String()),
		})
		if err != nil {
			t.Fatalf("200 节点大文档应通过: %v", err)
		}
		if !json.Valid(res.Document) {
			t.Fatal("规范化输出应为合法 JSON")
		}
	})

	t.Run("NodeNameWithScriptAccepted", func(t *testing.T) {
		// 节点编辑元数据 name 不做脚本过滤（仅长度校验），语义上原文保存；
		// 注意 validateDocument 经 json.Marshal 规范化会把 < 转义为 \u003c，需按语义断言。
		doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.container","props":{"tag":"div","layout":{"engine":"flex","flex":{}}},"name":"<script>alert(1)</script>"}]}`
		res, err := e.svc.Create(ctx, &blockdto.CreateReq{
			ProjectID: e.projectID, Name: "脚本节点", Document: json.RawMessage(doc),
		})
		if err != nil {
			t.Fatalf("节点 name 含脚本应通过结构校验: %v", err)
		}
		var page struct {
			Root []struct {
				Name string `json:"name"`
			} `json:"root"`
		}
		if err := json.Unmarshal(res.Document, &page); err != nil || len(page.Root) != 1 {
			t.Fatalf("文档反序列化失败: %v", err)
		}
		if page.Root[0].Name != "<script>alert(1)</script>" {
			t.Fatalf("节点 name 语义被改写: %q", page.Root[0].Name)
		}
	})

	t.Run("WhitespaceKindEquivalentToNoFilter", func(t *testing.T) {
		// 空白 kind 经 TrimSpace 变空串，语义等价于不传 kind（无过滤）。
		all, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID})
		if err != nil {
			t.Fatalf("全量列表应无错误: %v", err)
		}
		res, err := e.svc.List(ctx, &blockdto.ListReq{ProjectID: e.projectID, Kind: " \t "})
		if err != nil {
			t.Fatalf("空白 kind 过滤应无错误: %v", err)
		}
		if len(res) != len(all) {
			t.Fatalf("空白 kind 应等价于无过滤: 无过滤 %d 个, 空白 kind %d 个", len(all), len(res))
		}
	})
}

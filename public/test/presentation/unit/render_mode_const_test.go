// Package unit 渲染模式常量镜像测试（迁移 282，商品页双轨）。
//
// 常量在两处镜像：真值在 presentationmodel，跨模块消费者（商品页）只能 import
// presentationdto —— 模块间只允许依赖 contract 与不可变 DTO。两处不一致时**编译期不会报错**，
// 只会让消费侧的「renderMode == document」永远为假：页面明明已是独立文档，徽标却显示
// 「跟随模板」，用户据此以为模板改动还会同步过来。
package unit

import (
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
)

func TestRenderModeConstantsMirrorModel(t *testing.T) {
	if presentationdto.RenderModeTemplate != presentationmodel.RenderModeTemplate {
		t.Fatalf("dto.RenderModeTemplate(%q) 与 model.RenderModeTemplate(%q) 不一致",
			presentationdto.RenderModeTemplate, presentationmodel.RenderModeTemplate)
	}
	if presentationdto.RenderModeDocument != presentationmodel.RenderModeDocument {
		t.Fatalf("dto.RenderModeDocument(%q) 与 model.RenderModeDocument(%q) 不一致",
			presentationdto.RenderModeDocument, presentationmodel.RenderModeDocument)
	}
	// 归一化兜底：空值与未知值按 template（不经过迁移的测试数据 / 手工插入）。
	if presentationmodel.NormalizeRenderMode("") != presentationmodel.RenderModeTemplate {
		t.Fatalf("空渲染模式应归一化为 template")
	}
	if !presentationmodel.IsDocumentMode(presentationmodel.RenderModeDocument) {
		t.Fatalf("IsDocumentMode 应识别 document")
	}
	if presentationmodel.IsDocumentMode("") {
		t.Fatalf("空渲染模式不应被判为 document 模式")
	}
}

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

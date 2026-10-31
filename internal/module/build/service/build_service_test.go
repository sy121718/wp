package buildservice

import (
	"context"
	"errors"
	"testing"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
)

func TestCanonicalSourceID(t *testing.T) {
	const canonical = "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"
	for _, raw := range []string{
		canonical,
		"  3F0F1E3A-6D9A-4C0F-9A7E-6F6B1C2D3E4F  ",
		"3f0f1e3a6d9a4c0f9a7e6f6b1c2d3e4f",
		"{3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f}",
	} {
		got, ok := canonicalSourceID(raw)
		if !ok {
			t.Fatalf("%q 应被接受", raw)
		}
		if got != canonical {
			t.Fatalf("%q 应归一成 %q，实际 %q", raw, canonical, got)
		}
	}
	for _, raw := range []string{"", "   ", "not-a-uuid", "3f0f1e3a-6d9a-4c0f-9a7e", canonical + "0"} {
		if got, ok := canonicalSourceID(raw); ok {
			t.Fatalf("%q 应被拒绝，实际归一成 %q", raw, got)
		}
	}
}

func TestEnqueueRejectsNonUUIDSourceID(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	for _, c := range []struct {
		sourceType, sourceID string
	}{
		{buildmodel.SourceTypePage, ""},
		{buildmodel.SourceTypePage, "   "},
		{buildmodel.SourceTypePage, "page-1"},
		{buildmodel.SourceTypePresentation, "3f0f1e3a-6d9a-4c0f-9a7e"},
	} {
		_, _, err := svc.Enqueue(context.Background(), &builddto.EnqueueReq{
			SourceType: c.sourceType, SourceID: c.sourceID, DraftVersion: 1,
		})
		if err == nil {
			t.Fatalf("(%s,%q) 应被拒绝", c.sourceType, c.sourceID)
		}
		if !errors.Is(err, buildenums.Err(buildenums.ErrInvalidParam)) {
			t.Fatalf("(%s,%q) 应报 ErrInvalidParam，实际 %v", c.sourceType, c.sourceID, err)
		}
	}
}

func TestEnqueueRejectsMenuStaleFromGenericEntry(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	_, _, err := svc.Enqueue(context.Background(), &builddto.EnqueueReq{
		SourceType: buildmodel.SourceTypeMenuStale,
		SourceID:   "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f:header",
	})
	if !errors.Is(err, buildenums.Err(buildenums.ErrInvalidSource)) {
		t.Fatalf("menu_stale 从通用入口入队应报 ErrInvalidSource，实际 %v", err)
	}
}

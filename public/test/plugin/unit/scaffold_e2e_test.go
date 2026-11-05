package unit

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"go_wp/pkg/scaffold"
)

func TestScaffoldEndToEnd(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	files, err := scaffold.Files("marketing")
	if err != nil {
		t.Fatalf("scaffold.Files: %v", err)
	}
	zipBytes := zipFiles(t, files)

	if _, err := svc.Install(ctx, zipBytes); err != nil {
		t.Fatalf("脚手架产物安装失败: %v", err)
	}

	asm, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("EnabledAssembly: %v", err)
	}
	if _, ok := asm.Specs["plugin.marketing.marketing_card"]; !ok {
		t.Fatalf("缺组件规格 plugin.marketing.marketing_card，规格数=%d", len(asm.Specs))
	}

	if len(asm.Presets) != 1 || asm.Presets[0].ID != "marketing-hero" {
		t.Fatalf("预设未就位: %+v", asm.Presets)
	}

	list, _ := svc.List(ctx)
	if len(list) != 1 || list[0].SchemaVersion != 1 {
		t.Fatalf("schema 记账错误: %+v", list)
	}
}

func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip Create: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip Write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

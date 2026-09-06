package unit

// 脚手架端到端验收：scaffold.Files 生成的模板 → 打包 zip → Install →
// 组件规格 + 预设 + L1 schema 迁移全部就位（docs/06 全链路闭环）。

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"go_wp/internal/module/plugin/scaffold"
)

// TestScaffoldEndToEnd 脚手架产物可被 Install 完整接受（组件/预设/迁移）。
func TestScaffoldEndToEnd(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 脚手架生成文件 → 打包 zip（含 manifest + 组件模板 + migrations SQL）。
	files, err := scaffold.Files("marketing")
	if err != nil {
		t.Fatalf("scaffold.Files: %v", err)
	}
	zipBytes := zipFiles(t, files)

	// 安装（走真实 Install：安全解包 + manifest 校验 + 迁移执行 + 记账）。
	if _, err := svc.Install(ctx, zipBytes); err != nil {
		t.Fatalf("脚手架产物安装失败: %v", err)
	}

	// 组件规格就位（编译装配素材）。
	asm, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("EnabledAssembly: %v", err)
	}
	if _, ok := asm.Specs["plugin.marketing.marketing_card"]; !ok {
		t.Fatalf("缺组件规格 plugin.marketing.marketing_card，规格数=%d", len(asm.Specs))
	}
	// 预设就位。
	if len(asm.Presets) != 1 || asm.Presets[0].ID != "marketing-hero" {
		t.Fatalf("预设未就位: %+v", asm.Presets)
	}
	// L1 schema 迁移就位：registry 记 schema_version=1。
	list, _ := svc.List(ctx)
	if len(list) != 1 || list[0].SchemaVersion != 1 {
		t.Fatalf("schema 记账错误: %+v", list)
	}
}

// zipFiles 内存打包（复用 Files 的相对路径 map）。
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

// Package unit 插件服务 feature 测试（真实 PostgreSQL）：安装→列表→启停→卸载闭环。
package unit

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"

	plugindto "go_wp/internal/module/plugin/dto"

	"go_wp/public/test/support"
)

// 最小合法插件 zip（manifest + 一个组件模板）。
func pluginZip(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"manifest.json":                manifest,
		"components/campaign_card.jet": "<article class='{{ .Classes }}'>{{ .V.title }}</article>",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

const testManifest = `{
  "id": "marketing",
  "name": "营销组件",
  "version": "1.0.0",
  "components": [{
    "name": "campaign_card",
    "label": "活动卡片",
    "template": "campaign_card.jet",
    "props": {"title": {"kind": "text", "label": "标题", "default": "促销"}}
  }]
}`

// newService 隔离测试库 + 装配 service。
//
// 表结构来自生产迁移（plugin_registry 由 040 建，manifest 列是 jsonb）：AutoMigrate 会照
// model 的 []byte 字段建成 bytea，与生产分叉 —— 实测表现为「column "manifest" is of type
// bytea but expression is of type jsonb」。
func newService(t *testing.T) *pluginservice.Service {
	t.Helper()
	// 插件存储隔离到临时目录（避免相对路径 public/runtime/plugins 污染测试目录）。
	t.Setenv("GO_WP_PLUGIN_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	return pluginservice.NewService(pluginmodel.NewModel(db))
}

// TestInstallListToggleUninstall 完整生命周期。
func TestInstallListToggleUninstall(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 安装。
	if _, err := svc.Install(ctx, pluginZip(t, testManifest)); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	// 列表。
	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("列表失败: %v len=%d", err, len(list))
	}
	if list[0].ID != "marketing" || list[0].Version != "1.0.0" || !list[0].Enabled {
		t.Fatalf("列表行错误: %+v", list[0])
	}
	if list[0].ComponentCount != 1 {
		t.Fatalf("组件数应为 1: %d", list[0].ComponentCount)
	}
	// 装配素材。
	asm, err := svc.EnabledAssembly(ctx)
	if err != nil || len(asm.Specs) != 1 {
		t.Fatalf("装配素材错误: %v specs=%d", err, len(asm.Specs))
	}
	if _, ok := asm.Specs["plugin.marketing.campaign_card"]; !ok {
		t.Fatalf("缺组件规格")
	}
	// 停用。
	if err := svc.Toggle(ctx, &plugindto.ToggleReq{ID: "marketing", Enabled: false}); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	asmOff, _ := svc.EnabledAssembly(ctx)
	if len(asmOff.Specs) != 0 {
		t.Fatalf("停用后装配素材应为空")
	}
	// 卸载。
	if err := svc.Uninstall(ctx, &plugindto.UninstallReq{ID: "marketing"}); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	list2, _ := svc.List(ctx)
	if len(list2) != 0 {
		t.Fatalf("卸载后列表应为空")
	}
}

// TestInstallRejectsBadPackage 坏包（缺 manifest/穿越）安装拒绝。
func TestInstallRejectsBadPackage(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 缺 manifest。
	if _, err := svc.Install(ctx, pluginZip(t, `{"id":"x","name":"x","version":"1.0.0","components":[]}`)); err == nil {
		t.Fatalf("无组件 manifest 应拒绝")
	}
	// 非法 manifest（ID 大写）。
	bad := `{"id":"X","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet"}]}`
	if _, err := svc.Install(ctx, pluginZip(t, bad)); err == nil {
		t.Fatalf("非法 manifest 应拒绝")
	}
	// 空字节。
	if _, err := svc.Install(ctx, nil); err == nil {
		t.Fatalf("空包应拒绝")
	}
}

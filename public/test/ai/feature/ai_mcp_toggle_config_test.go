package feature

import (
	"context"
	"testing"

	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 对外接入点开关是一次「读整组 → 改一个键 → 带版本号写回」。
//
// 这类读-改-写最危险的失效不是报错，而是**静默覆盖别人的改动**：
//   - 用读到的旧版本把整组写回去 → 别人刚改的键被抹掉；
//   - 或者版本号没带上 → 乐观锁形同虚设，谁最后写谁赢。
//
// 本用例把两条都钉住：同组里别的键必须原样保留；版本过期必须被服务端打回，
// 且打回之后库里一个字节都没变。
func TestMcpToggleKeepsOtherKeysAndRejectsStaleVersion(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	ctx := context.Background()

	// AI 配置组是 548 的 **seed**（`mcp_enabled = false`，version = 1）。
	// 迁移模板库只跑 migrations.Run，不跑 RunSeeds —— 生产启动是两者都跑（cmd/main.go 与
	// internal/routers/assembly.go），所以这里必须补一次，否则库内根本没有 ai 组，
	// 首次开启会撞 ErrGroupNotFound 而不是本用例要验的读-改-写。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("补种生产 seed 失败（ai 配置组由 548 种下）：%v", err)
	}

	// 再塞一个同组的其它键：它代表「别人刚改过的东西」，本用例要证明它不会被抹掉。
	res := db.Exec(
		"UPDATE sys_config SET config_data = config_data || '{\"keep_me\":\"original\"}'::jsonb " +
			"WHERE group_key = 'ai'")
	if res.Error != nil {
		t.Fatalf("准备同组其它键失败：%v", res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("准备同组其它键应当命中 1 行（ai 组），实际 %d 行 —— 说明 seed 没种上", res.RowsAffected)
	}

	cfg := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(db), nil)
	tokens := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))

	if tokens.McpEnabled(ctx, cfg) {
		t.Fatal("默认应为关闭（548 迁移里是 false）")
	}

	if err := tokens.SetMcpEnabled(ctx, cfg, true, 7); err != nil {
		t.Fatalf("首次开启应成功：%v", err)
	}
	on, err := cfg.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if v, _ := on.Data[sysconfigcontract.KeyMCPEnabled].(bool); !v {
		t.Fatalf("开关应已打开，实际 %v", on.Data)
	}
	if on.Data["keep_me"] != "original" {
		t.Fatalf("同组的其它键被抹掉了：%v", on.Data)
	}
	if !tokens.McpEnabled(ctx, cfg) {
		t.Fatal("读侧应与写入一致")
	}

	// 模拟并发：有人拿着更早的版本号把整组又写了一遍，服务端版本已经前进。
	// 这正是「用旧版本覆盖」的场景 —— 必须被乐观锁打回。
	stale := on.Version - 1
	if _, err := cfg.SetGroup(ctx, &sysconfigdto.SetGroupReq{
		GroupKey: sysconfigcontract.GroupAI,
		Data:     map[string]any{"someone_else": true},
		Version:  stale,
	}); err == nil || err.Error() != sysconfigenums.ErrVersionConflict {
		t.Fatalf("旧版本写入应报版本冲突，实际：%v", err)
	}

	// 打回之后库里必须一个字都没变：开关还是 on、别人的键还在。
	after, err := cfg.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if v, _ := after.Data[sysconfigcontract.KeyMCPEnabled].(bool); !v {
		t.Fatalf("冲突写入不得改动开关：%v", after.Data)
	}
	if after.Data["keep_me"] != "original" {
		t.Fatalf("冲突写入不得抹掉同组其它键：%v", after.Data)
	}

	// 关掉再读：两个方向都要能改。
	if err := tokens.SetMcpEnabled(ctx, cfg, false, 7); err != nil {
		t.Fatalf("关闭应成功：%v", err)
	}
	if tokens.McpEnabled(ctx, cfg) {
		t.Fatal("关闭后读侧应为 false")
	}
	off, err := cfg.GetGroup(ctx, sysconfigcontract.GroupAI)
	if err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	if off.Data["keep_me"] != "original" {
		t.Fatalf("关闭也不得抹掉同组其它键：%v", off.Data)
	}
}

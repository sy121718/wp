package buildservice

// build_menu_stale_test.go — menu_stale 入队的**参数规则**（不碰数据库）。
//
// 这里只测「在打开任何事务之前就能判定的事」：kind 白名单、工程 id 的形状、任务行的
// 组装规则。事务语义（同事务回滚、版本递增、并发）在 public/test/build/feature 里
// 用真实 PG 验证 —— 那部分离开库就没有意义。

import (
	"context"
	"errors"
	"testing"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
)

// TestEnqueueMenuStaleRejectsInvalidKind 四值之外的位置在取号之前被拒（不消耗版本号）。
func TestEnqueueMenuStaleRejectsInvalidKind(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	for _, kind := range []string{"", "sidebar", "HEADER", "header :mobile", "header_mobile2"} {
		_, err := svc.EnqueueMenuStaleTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", kind)
		if err == nil {
			t.Fatalf("菜单位置 %q 应被拒绝", kind)
		}
		// 判定必须是「位置非法」这一条事实，而不是被后面的「没有事务」抢先命中。
		// facing 的 Facts 支持 errors.Is 按 key 比较，所以这里不必拿错误对象做比对。
		if !errors.Is(err, buildenums.Err(buildenums.ErrInvalidMenuKind)) {
			t.Fatalf("菜单位置 %q 应报 ErrInvalidMenuKind，实际 %v", kind, err)
		}
	}
}

// TestEnqueueMenuStaleRejectsInvalidProject 工程 id 不是 uuid 时同样在取号之前拒绝。
func TestEnqueueMenuStaleRejectsInvalidProject(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	for _, id := range []string{"", "  ", "not-a-uuid", "3f0f1e3a-6d9a-4c0f-9a7e"} {
		_, err := svc.EnqueueMenuStaleTx(context.Background(), nil, id, "header")
		if err == nil {
			t.Fatalf("工程 id %q 应被拒绝", id)
		}
		if !errors.Is(err, buildenums.Err(buildenums.ErrInvalidParam)) {
			t.Fatalf("工程 id %q 应报 ErrInvalidParam，实际 %v", id, err)
		}
	}
}

// TestEnqueueMenuStaleRejectsMissingTx 没有事务句柄时拒绝：本入口不允许自取连接库。
//
// 用 nil 句柄驱动真实 service（而不是 NewService(nil)）：判定必须落在事务检查上，
// 而不是「service 没 model」这条更早的分支 —— 否则调用方传 nil tx 会得到
// 一个与「参数错误」有关、与「事务缺失」无关的结论。
func TestEnqueueMenuStaleRejectsMissingTx(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	_, err := svc.EnqueueMenuStaleTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "header")
	if !errors.Is(err, buildmodel.ErrTxRequired) {
		t.Fatalf("没有事务句柄时必须报 ErrTxRequired，实际 %v", err)
	}
}

// TestMenuStaleInputHashShape 输入键按「内容键#版本号」组装，版本号是最小变动分量。
func TestMenuStaleInputHashShape(t *testing.T) {
	pid := "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"
	v1 := menuStaleInputHash(pid, "header", 1)
	v2 := menuStaleInputHash(pid, "header", 2)
	footer := menuStaleInputHash(pid, "footer", 1)
	if v1 == v2 {
		t.Fatal("同一位置的不同版本必须得到不同的输入键")
	}
	if v1 == footer {
		t.Fatal("不同位置必须是不同的输入键")
	}
	if want := "menu_stale:" + pid + ":header#1"; v1 != want {
		t.Fatalf("输入键形状不符: got=%q want=%q", v1, want)
	}
}

// TestBuildEnumsMenuKindFallback 事实 key 与兜底逐字对齐（出口取词缺词条时用它）。
func TestBuildEnumsMenuKindFallback(t *testing.T) {
	if buildenums.FallbackFor(buildenums.ErrInvalidMenuKind) == "" {
		t.Fatal("ErrInvalidMenuKind 必须有中文兜底")
	}
	if buildenums.FallbackFor(buildenums.ErrInvalidSource) == "" {
		t.Fatal("既有来源错误必须仍有兜底（对照组：新 key 的加入不能挤掉旧 key）")
	}
	if got, ok := builddto.MenuStaleSourceID("3F0F1E3A-6D9A-4C0F-9A7E-6F6B1C2D3E4F", "header"); !ok ||
		got != "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f:header" {
		t.Fatalf("source_id 必须归一到规范 UUID 形态: got=%q ok=%v", got, ok)
	}
}

package buildservice

import (
	"context"
	"errors"
	"testing"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	"go_wp/pkg/facing"
)

func TestEnqueueMenuStaleRejectsInvalidKind(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	for _, kind := range []string{"", "sidebar", "HEADER", "header :mobile", "header_mobile2"} {
		_, err := svc.EnqueueMenuStaleTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", kind)
		if err == nil {
			t.Fatalf("菜单位置 %q 应被拒绝", kind)
		}

		if !errors.Is(err, buildenums.Err(buildenums.ErrInvalidMenuKind)) {
			t.Fatalf("菜单位置 %q 应报 ErrInvalidMenuKind，实际 %v", kind, err)
		}
	}
}

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

func TestEnqueueMenuStaleRejectsMissingTx(t *testing.T) {
	svc := NewService(buildmodel.NewModel(nil))
	_, err := svc.EnqueueMenuStaleTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "header")
	if !errors.Is(err, buildmodel.ErrTxRequired) {
		t.Fatalf("没有事务句柄时必须报 ErrTxRequired，实际 %v", err)
	}
}

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

func TestBuildEnumsDeclareKeysWithoutBundledText(t *testing.T) {
	// 声明处只给 key + 状态码，成品句归词库：这里断言的是「不再自带中文兜底」这个
	// 真实形态。若哪天有人把成品句塞回 enums，这条会立刻红。
	for _, key := range []string{buildenums.ErrInvalidMenuKind, buildenums.ErrInvalidSource} {
		fact, ok := facing.FactOf(buildenums.Err(key))
		if !ok || !fact.Business {
			t.Fatalf("%s 必须声明为业务错误，实际 %+v（ok=%v）", key, fact, ok)
		}
		if fact.Key != key {
			t.Fatalf("%s 的 key 形状不符: %q", key, fact.Key)
		}
		if fact.Fallback != fact.Key {
			t.Fatalf("%s 不该自带成品句兜底，实际兜底 %q", key, fact.Fallback)
		}
	}
	if got, ok := builddto.MenuStaleSourceID("3F0F1E3A-6D9A-4C0F-9A7E-6F6B1C2D3E4F", "header"); !ok ||
		got != "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f:header" {
		t.Fatalf("source_id 必须归一到规范 UUID 形态: got=%q ok=%v", got, ok)
	}
}

package buildservice

// build_service_test.go — Enqueue 的**入参形状**规则（不碰数据库）。
//
// 迁移 698 把 build_jobs.source_id 从 uuid 放宽成 text 之后，数据库不再替非规范写法
// 归一（uuid 列会归一大写 / 无连字符，text 列逐字比较）。这两条不变量的失效方式是静默的：
// 同来源两条 running（互斥失效）、同一份工作两条 pending（去重失效），都不会报错。
// 所以归一与非法值拒绝必须在 service 层测，且要在打开任何连接之前就能判定
// （测试用 NewService(buildmodel.NewModel(nil))：任何走到 SQL 的路径都会 panic，
// 能返回业务错误就说明判定发生在地库之前）。

import (
	"context"
	"errors"
	"testing"

	builddto "go_wp/internal/module/build/dto"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
)

// TestCanonicalSourceID 来源 id 的归一口径：大小写 / 无连字符 / 花括号都落到同一个规范文本。
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

// TestEnqueueRejectsNonUUIDSourceID 非法来源 id 在入队前被拒（参数错误），不会进 SQL。
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

// TestEnqueueRejectsMenuStaleFromGenericEntry menu_stale 只能走同事务专用入口。
//
// 通用 Enqueue 若是接受 menu_stale，就会出现「不带号源、不在业务事务里」的 menu_stale 行：
// 版本号为空 / 重复，uq_build_jobs_context_version 与 shape CHECK 两边都拦不住「合法但没意义」的任务。
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

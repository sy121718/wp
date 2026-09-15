package unit

// presentation_archive_role_test.go — 归档实例的角色维度（审计 EDT-004 第二层）。
//
// 同一个实体可以同时有两张页面：详情页（讲这个分类/品牌）与归档页（列它下面的内容）。
// 此前 presentation_instances 的唯一键只到实体，于是第二个实例会被当成「已存在」
// 直接返回第一个 —— 建归档页的动作静默变成「拿到详情页实例」，调用方还以为建成了。
//
// 本用例钉住三件事：两种角色的实例能共存、同角色重复创建仍是幂等的、
// 非法角色被拒绝（不能悄悄落到 detail 上）。

import (
	"context"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
)

func TestInstanceRolesCoexistForSameEntity(t *testing.T) {
	f := newPresFixture(t)
	ctx := context.Background()
	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "role-coexist",
		Data: map[string]any{"title": "角色共存", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}

	// 1) 不传 role：既有调用方语义 —— 默认详情页。
	detail, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/roles/detail",
	})
	if err != nil {
		t.Fatalf("创建详情页实例失败: %v", err)
	}
	if detail.InstanceRole != "detail" {
		t.Fatalf("不传角色时应落到 detail，实际 %q", detail.InstanceRole)
	}

	// 2) 同实体 + 不同角色：必须能共存（这正是本次改造要解决的）。
	archive, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/roles/archive", InstanceRole: "archive",
	})
	if err != nil {
		t.Fatalf("创建归档页实例失败（同实体不同角色应可共存）: %v", err)
	}
	if archive.ID == detail.ID {
		t.Fatalf("归档实例与详情实例应是两条记录，实际都返回 %s", archive.ID)
	}
	if archive.InstanceRole != "archive" {
		t.Fatalf("归档实例的角色应为 archive，实际 %q", archive.InstanceRole)
	}

	// 3) 同实体同角色再建：幂等（返回已有实例，而不是报唯一键冲突）。
	again, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/roles/archive", InstanceRole: "archive",
	})
	if err != nil {
		t.Fatalf("同角色重复创建应幂等: %v", err)
	}
	if again.ID != archive.ID {
		t.Fatalf("同角色重复创建应返回既有实例 %s，实际 %s", archive.ID, again.ID)
	}

	// 4) 非法角色：明确拒绝，不能静默当成 detail（否则「我要归档页」会建出详情页）。
	if _, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/roles/bogus", InstanceRole: "nonsense",
	}); err == nil {
		t.Fatalf("非法实例角色应被拒绝")
	}
}

package unit

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

	if _, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/roles/bogus", InstanceRole: "nonsense",
	}); err == nil {
		t.Fatalf("非法实例角色应被拒绝")
	}
}

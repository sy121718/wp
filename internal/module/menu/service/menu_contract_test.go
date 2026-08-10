package menuservice

import (
	"context"
	"testing"

	menudto "go_wp/internal/module/menu/dto"
	menumodel "go_wp/internal/module/menu/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestValidateComponentBinding(t *testing.T) {
	tests := []struct {
		name      string
		menuType  int
		component string
		wantErr   bool
	}{
		{name: "页面菜单完整路径", menuType: 2, component: "/src/views/system/help_document/index.vue"},
		{name: "页面菜单缺少组件", menuType: 2, wantErr: true},
		{name: "页面菜单使用短路径", menuType: 2, component: "system/help_document/index", wantErr: true},
		{name: "页面菜单越出视图目录", menuType: 2, component: "/src/layout/index.vue", wantErr: true},
		{name: "目录不绑定组件", menuType: 1},
		{name: "目录错误绑定组件", menuType: 1, component: "/src/views/system/index.vue", wantErr: true},
		{name: "按钮不绑定组件", menuType: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComponentBinding(tt.menuType, tt.component)
			if (err != nil) != tt.wantErr {
				t.Fatalf("组件路径校验结果不正确，err=%v", err)
			}
		})
	}
}

func TestBuildRouteNodesProjectsButtonAuthsToParentMenu(t *testing.T) {
	nodes := []menudto.TreeNode{
		{
			ID:    1,
			Title: "系统管理",
			Type:  1,
			Children: []menudto.TreeNode{
				{
					ID:             2,
					Title:          "角色管理",
					Type:           2,
					Path:           "/system/role",
					Component:      "/src/views/role/index.vue",
					PermissionCode: "role:list",
				},
			},
		},
	}

	routes := buildRouteNodes(nodes, map[uint64][]string{
		2: {"role:user:list", "role:user:save"},
	})
	if len(routes) != 1 || len(routes[0].Children) != 1 {
		t.Fatalf("路由树结构异常：%+v", routes)
	}
	auths := routes[0].Children[0].Meta.Auths
	want := []string{"role:list", "role:user:list", "role:user:save"}
	if len(auths) != len(want) {
		t.Fatalf("按钮权限投影数量不正确：%v", auths)
	}
	for index, code := range want {
		if auths[index] != code {
			t.Fatalf("按钮权限投影不正确，期望 %v，实际 %v", want, auths)
		}
	}
}

func TestUpdateButtonCanClearParentMenu(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	if err = db.AutoMigrate(&menumodel.MenuEntity{}); err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}

	ctx := context.Background()
	model := menumodel.NewMenuModel(db)
	button := &menumodel.MenuEntity{
		PermissionCode: stringPtr("role:create"),
		Title:          "新增角色",
		ParentID:       10,
		Type:           menumodel.MenuTypeButton,
		Status:         menumodel.MenuStatusEnabled,
	}
	if err = db.Session(&gorm.Session{SkipHooks: true}).Create(button).Error; err != nil {
		t.Fatalf("创建按钮菜单失败：%v", err)
	}

	svc := NewService(model, nil)
	if err = svc.Update(ctx, &menudto.UpdateReq{
		ID:             button.ID,
		PermissionCode: "role:create",
		Title:          button.Title,
		ParentID:       0,
		Type:           menumodel.MenuTypeButton,
		Status:         menumodel.MenuStatusEnabled,
	}); err != nil {
		t.Fatalf("清除按钮上级菜单失败：%v", err)
	}

	var parentID uint64
	if err = db.Model(&menumodel.MenuEntity{}).
		Select("parent_id").
		Where("id = ?", button.ID).
		Scan(&parentID).Error; err != nil {
		t.Fatalf("查询更新后的按钮菜单失败：%v", err)
	}
	if parentID != 0 {
		t.Fatalf("上级菜单关联未清除，实际 parent_id：%d", parentID)
	}
}

func stringPtr(value string) *string {
	return &value
}

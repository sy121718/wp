package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"reflect"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

func newProjectService(t *testing.T) *projectservice.Service {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	return projectservice.NewService(projectmodel.NewProjectModel(db))
}

func TestProjectCreateUpdateAndExists(t *testing.T) {
	svc := newProjectService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, &projectdto.CreateReq{Name: "  官网工程  ", Settings: json.RawMessage(`{"locale":"zh-CN"}`)})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}

	if created.Name != "官网工程" || !jsonEqual(t, created.Settings, `{"locale":"zh-CN"}`) {
		t.Fatalf("工程字段未规范化: %+v", created)
	}
	exists, err := svc.Exists(ctx, created.ID)
	if err != nil || !exists {
		t.Fatalf("已创建工程应存在: exists=%t err=%v", exists, err)
	}
	updated, err := svc.Update(ctx, &projectdto.UpdateReq{ID: created.ID, Name: "新官网", Settings: json.RawMessage(`{"theme":"dark"}`)})
	if err != nil {
		t.Fatalf("更新工程失败: %v", err)
	}
	if updated.Name != "新官网" || !jsonEqual(t, updated.Settings, `{"theme":"dark"}`) {
		t.Fatalf("工程更新结果错误: %+v", updated)
	}
}

func jsonEqual(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("期望值不是合法 JSON: %s", want)
	}
	return reflect.DeepEqual(g, w)
}

func TestProjectRejectsInvalidSettings(t *testing.T) {
	svc := newProjectService(t)
	_, err := svc.Create(context.Background(), &projectdto.CreateReq{Name: "工程", Settings: json.RawMessage(`[]`)})
	if err == nil || !strings.Contains(err.Error(), projectenums.ErrInvalidSettings) {
		t.Fatalf("数组设置应被拒绝: %v", err)
	}
}

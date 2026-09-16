package rlstest

// rls_navigation_scope_test.go — navigation 模块**只带 id 的入口**的工程作用域护栏（DB-009）。
//
// navigations 在迁移 215 里带 FORCE 策略，而这条链路的请求里只有导航项 id：
// 后台导航管理页的「更新 / 详情 / 删除」都不带工程。原先它们走 model 的「不限工程」分支
// （projectID 传空）—— 不设 app.project_id 的语句在非超级角色下**静默匹配 0 行**
//（读到 ErrRecordNotFound、改 0 行、删 0 行且不报错），表现为「导航项明明在，却报不存在」，
// 日志里什么都没有。
//
// 现在的形态：service 逐工程独立作用域探测出归属（id 是主键，跨工程不会重复命中），
// 拿到实体自带的 project_id 之后写入与回读都受策略约束；工程清单为空时**显式失败**。
//
// 每条断言都具备失败能力（不是「调用没报错」）：
//   - 未设作用域时按 id 读不到（fail closed）—— 把策略删了/放宽了立刻红；
//   - 逐工程定位必须命中正确的那个工程（漏改回「不限工程」后这里立刻红：定位跳读不到行）；
//   - 跨工程拿 A 的作用域读 B 的行必须读不到；
//   - 定位后的写入必须真的落到 B 的行上（回读校验，不是只看返回没报错）；
//   - 工程表为空时显式 ErrProjectRequired，不退化成静默的 ErrNotFound。
//
// 全程跑在非超级角色下（rlsFixture 保证，并用 rls.BypassedRole 自检）。

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// seedNavigation 经 model 的写入路径（带工程作用域）落一行导航项。
//
// 刻意经 model.Create 而不是裸 INSERT：非超级角色下不带 app.project_id 的写入会被策略的
// WITH CHECK 拒绝 —— 种子数据本身就在证明「写入路径必须作用域化」。
func seedNavigation(t *testing.T, db *gorm.DB, projectID, title, path string) string {
	t.Helper()
	id := uuid.NewString()
	err := navigationmodel.NewModel(db).Create(context.Background(), &navigationmodel.NavigationEntity{
		ID: id, ProjectID: projectID, Title: title, Path: path,
		Kind: "header", SourceType: "custom", Target: "self",
		CreatedAt: timeNow(), UpdatedAt: timeNow(),
	})
	if err != nil {
		t.Fatalf("写入导航项失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id
}

// navigationScopeFixture 造「非超级角色 + 两个工程 + navigation service（注入工程契约）」。
func navigationScopeFixture(t *testing.T) (*gorm.DB, *navigationservice.Service, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

// TestRLS_NavigationLocate_NoScopeFailsClosed 无工程变量时按 id 一律读不到。
//
// 这条钉住的是改造动机本身：不设 app.project_id 的「不限工程」定位在非超级角色下
// 静默返回 0 行 —— 功能表现为「导航项突然不存在」，没有任何错误日志。
func TestRLS_NavigationLocate_NoScopeFailsClosed(t *testing.T) {
	db, _, pA, pB := navigationScopeFixture(t)
	ctx := context.Background()
	idB := seedNavigation(t, db, pB, "B 的菜单", "/b")

	// 1) 绕开 model 的作用域包装裸查（等价于「没改造的路径」）：策略谓词为 NULL ⇒ 0 行。
	var raw int64
	if err := db.Table("navigations").Where("id = ?", idB).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}

	// 2) model 的「不限工程」分支（projectID 传空）就是改造前那条定位形态：
	//    读不到行，且**不会**读到别的工程的行（方向安全，但功能静默失效）。
	if _, err := navigationmodel.NewModel(db).Get(ctx, "", idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("缺作用域按 id 直查应 ErrRecordNotFound（fail closed），实际 %v", err)
	}

	// 3) 跨工程：拿 A 的作用域读 B 的行必须读不到；拿 B 的作用域读得到。
	if _, err := navigationmodel.NewModel(db).Get(ctx, pA, idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿工程 A 的作用域读工程 B 的导航项应 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := navigationmodel.NewModel(db).Get(ctx, pB, idB); err != nil {
		t.Fatalf("拿工程 B 的作用域读本工程导航项应成功，实际 %v", err)
	}
}

// TestRLS_NavigationLocate_ServiceReadsAcrossProjects 只带 id 的详情入口在多工程下可用。
//
// 这条是本次改动的核心断言：Get 的请求里只有 id，必须逐工程探测出归属。
// 把它改回「不限工程」直查（改造前形态），下面第一条断言立刻红 —— 失败能力所在。
func TestRLS_NavigationLocate_ServiceReadsAcrossProjects(t *testing.T) {
	db, svc, pA, pB := navigationScopeFixture(t)
	ctx := context.Background()
	idA := seedNavigation(t, db, pA, "A 的菜单", "/a")
	idB := seedNavigation(t, db, pB, "B 的菜单", "/b")

	res, err := svc.Get(ctx, &navigationdto.GetReq{ID: idB})
	if err != nil {
		t.Fatalf("多工程下按 id 取详情应成功（逐工程定位）: %v", err)
	}
	if res.ProjectID != pB {
		t.Fatalf("应定位到工程 B，实际 %s", res.ProjectID)
	}
	if res2, err := svc.Get(ctx, &navigationdto.GetReq{ID: idA}); err != nil || res2.ProjectID != pA {
		t.Fatalf("应定位到工程 A，实际 %+v（err=%v）", res2, err)
	}
	// 不存在的 id：逐工程全部未命中 → 模块的 ErrNotFound（不是静默的空响应）。
	if _, err := svc.Get(ctx, &navigationdto.GetReq{ID: uuid.NewString()}); err == nil ||
		err.Error() != navigationenums.ErrNotFound {
		t.Fatalf("不存在的导航项应 ErrNotFound，实际 %v", err)
	}
}

// TestRLS_NavigationLocate_ServiceWritesAcrossProjects 定位到归属后事务内可正常读写。
//
// 断言不只是「更新没报错」：必须回读到改动真的落在**工程 B 的那一行**上
// （换非超级角色后「没定位到」的写法会静默改 0 行并返回成功 —— 这条就是抓它）。
func TestRLS_NavigationLocate_ServiceWritesAcrossProjects(t *testing.T) {
	db, svc, _, pB := navigationScopeFixture(t)
	ctx := context.Background()
	idB := seedNavigation(t, db, pB, "B 的菜单", "/b-orig")

	title := "B 的菜单（改后）"
	if _, err := svc.Update(ctx, &navigationdto.UpdateReq{ID: idB, Title: &title}); err != nil {
		t.Fatalf("多工程下只带 id 更新应成功（逐工程定位 + 事务作用域）: %v", err)
	}
	e, err := navigationmodel.NewModel(db).Get(ctx, pB, idB)
	if err != nil {
		t.Fatalf("回读更新后的导航项失败: %v", err)
	}
	if e.Title != title {
		t.Fatalf("标题应已更新为 %q，实际 %q —— 更新静默落到 0 行", title, e.Title)
	}

	// 删除同样是只带 id 的入口：定位到归属后在本工程作用域内删掉自身。
	if err := svc.Delete(ctx, &navigationdto.DeleteReq{ID: idB}); err != nil {
		t.Fatalf("多工程下只带 id 删除应成功: %v", err)
	}
	if _, err := navigationmodel.NewModel(db).Get(ctx, pB, idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删除后应查不到，实际 %v", err)
	}
}

// TestRLS_NavigationLocate_EmptyProjectTableFailsExplicitly 一个工程都没有时显式失败。
//
// 静默返回 ErrNotFound 会把「读不到工程表」伪装成「导航项不存在」—— 那正是本批要消灭的
// fail-silent。这里用 TRUNCATE projects CASCADE 清空工程表模拟这种环境。
func TestRLS_NavigationLocate_EmptyProjectTableFailsExplicitly(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := navigationservice.NewService(navigationmodel.NewModel(db),
		projectservice.NewService(projectmodel.NewProjectModel(db)))

	_, err := svc.Get(context.Background(), &navigationdto.GetReq{ID: uuid.NewString()})
	if err == nil || err.Error() != navigationenums.ErrProjectRequired {
		t.Fatalf("无工程时定位应显式失败 ErrProjectRequired（而不是静默 ErrNotFound），实际 %v", err)
	}
}

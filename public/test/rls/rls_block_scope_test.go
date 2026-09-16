package rlstest

// rls_block_scope_test.go — block 模块**只带 id 的入口**的工程作用域护栏（DB-009）。
//
// blocks 在迁移 215 里带 FORCE 策略，而「更新 / 删除 / 复制 AST」这些入口的请求里只有块 id。
// 原先它们经 getExistingBlock 走 model 的「不限工程」分支（projectID 传空）—— 不设
// app.project_id 的语句在非超级角色下**静默匹配 0 行**（读到 ErrRecordNotFound、改 0 行、
// 删 0 行且不报错），表现为「块明明在，却报不存在」，日志里什么都没有。
//
// 现在的形态：service 逐工程独立作用域探测出归属（块 id 是主键，跨工程不会重复命中），
// 拿到实体自带的 project_id 之后写入与删除都受策略约束；工程清单为空时**显式失败**。
//
// 每条断言都具备失败能力（不是「调用没报错」）：
//   - 未设作用域时按 id 读不到（fail closed）—— 把策略删了/放宽了立刻红；
//   - 逐工程定位必须命中正确的那个工程（改回「不限工程」后这里立刻红）；
//   - 跨工程拿 A 的作用域读 B 的行必须读不到；
//   - 定位后的写入与删除必须真的作用在 B 的行上（回读校验，不是只看返回没报错）；
//   - 工程表为空时显式 ErrProjectRequired，不退化成静默的 ErrNotFound。
//
// 全程跑在非超级角色下（rlsFixture 保证，并用 rls.BypassedRole 自检）。

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// seedScopeBlock 经 model 的写入路径（带工程作用域）落一行块，文档是合法的页面文档结构。
//
// reuse_mode 用 template：一次性复制语义下删除/更新不触发引用检查与 stale 传播，
// 本用例要验的是**工程作用域**，不是引用编排。
func seedScopeBlock(t *testing.T, db *gorm.DB, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	err := blockmodel.NewBlockModel(db).Create(context.Background(), &blockmodel.BlockEntity{
		ID: id, ProjectID: projectID, Name: name,
		Kind: blockmodel.KindBlock, Category: blockmodel.DefaultCategory,
		ReuseMode:  blockmodel.ReuseTemplate,
		Document:   []byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		CreateTime: timeNow(), UpdatedAt: timeNow(),
	})
	if err != nil {
		t.Fatalf("写入块失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id
}

// blockScopeFixture 造「非超级角色 + 两个工程 + block service（注入工程契约）」。
func blockScopeFixture(t *testing.T) (*gorm.DB, *blockservice.Service, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

// TestRLS_BlockLocate_NoScopeFailsClosed 无工程变量时按 id 一律读不到。
func TestRLS_BlockLocate_NoScopeFailsClosed(t *testing.T) {
	db, _, pA, pB := blockScopeFixture(t)
	ctx := context.Background()
	idB := seedScopeBlock(t, db, pB, "B 的块")

	// 1) 绕开 model 的作用域包装裸查（等价于「没改造的路径」）：策略谓词为 NULL ⇒ 0 行。
	var raw int64
	if err := db.Table("blocks").Where("id = ?", idB).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}

	// 2) model 的「不限工程」分支（projectID 传空）就是改造前那条定位形态。
	if _, err := blockmodel.NewBlockModel(db).GetByID(ctx, idB, ""); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("缺作用域按 id 直查应 ErrRecordNotFound（fail closed），实际 %v", err)
	}

	// 3) 跨工程：拿 A 的作用域读 B 的行必须读不到；拿 B 的作用域读得到。
	if _, err := blockmodel.NewBlockModel(db).GetByID(ctx, idB, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿工程 A 的作用域读工程 B 的块应 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := blockmodel.NewBlockModel(db).GetByID(ctx, idB, pB); err != nil {
		t.Fatalf("拿工程 B 的作用域读本工程的块应成功，实际 %v", err)
	}
}

// TestRLS_BlockLocate_ServiceFindsOwnerAcrossProjects 只带 id 的入口在多工程下能定位到归属。
//
// 这条是本次改动的核心断言：Update / Delete / CloneAST 共用 getExistingBlock 这一跳定位，
// 请求里只有块 id。把它改回「不限工程」直查（改造前形态），下面第一条断言立刻红 ——
// 失败能力所在。
func TestRLS_BlockLocate_ServiceFindsOwnerAcrossProjects(t *testing.T) {
	db, svc, pA, pB := blockScopeFixture(t)
	ctx := context.Background()
	idA := seedScopeBlock(t, db, pA, "A 的块")
	idB := seedScopeBlock(t, db, pB, "B 的块")

	// Update 会先经定位跳取块：定位失败就会在此报「块不存在」。
	name := "B 的块（改后）"
	res, err := svc.Update(ctx, &blockdto.UpdateReq{ID: idB, Name: name})
	if err != nil {
		t.Fatalf("多工程下只带 id 更新应成功（逐工程定位 + 事务作用域）: %v", err)
	}
	if res.ProjectID != pB {
		t.Fatalf("应定位到工程 B，实际 %s", res.ProjectID)
	}
	resA, err := svc.Update(ctx, &blockdto.UpdateReq{ID: idA, Name: "A 的块（改后）"})
	if err != nil || resA.ProjectID != pA {
		t.Fatalf("应定位到工程 A，实际 %+v（err=%v）", resA, err)
	}

	// 不存在的 id：逐工程全部未命中 → 模块的 ErrNotFound（不是静默的空响应）。
	if _, err := svc.Update(ctx, &blockdto.UpdateReq{ID: uuid.NewString(), Name: "x"}); !errors.Is(err, blockservice.ErrNotFound) {
		t.Fatalf("不存在的块应 ErrNotFound，实际 %v", err)
	}
}

// TestRLS_BlockLocate_WritesLandOnOwnerRow 定位到归属后事务内可正常读写。
//
// 断言不只是「更新没报错」：必须回读到改动真的落在**工程 B 的那一行**上
// （换非超级角色后「没定位到」的写法会静默改 0 行并返回成功 —— 这条就是抓它）。
func TestRLS_BlockLocate_WritesLandOnOwnerRow(t *testing.T) {
	db, svc, _, pB := blockScopeFixture(t)
	ctx := context.Background()
	idB := seedScopeBlock(t, db, pB, "B 的块")

	name := "B 的块（改后）"
	if _, err := svc.Update(ctx, &blockdto.UpdateReq{ID: idB, Name: name}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	m := blockmodel.NewBlockModel(db)
	e, err := m.GetByID(ctx, idB, pB)
	if err != nil {
		t.Fatalf("回读更新后的块失败: %v", err)
	}
	if e.Name != name {
		t.Fatalf("块名应已更新为 %q，实际 %q —— 更新静默落到 0 行", name, e.Name)
	}

	// 删除同样是只带 id 的入口：定位到归属后在本工程作用域内删掉它。
	if err := svc.Delete(ctx, &blockdto.DeleteReq{ID: idB}); err != nil {
		t.Fatalf("多工程下只带 id 删除应成功: %v", err)
	}
	if _, err := m.GetByID(ctx, idB, pB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删除后应查不到，实际 %v", err)
	}
}

// TestRLS_BlockLocate_EmptyProjectTableFailsExplicitly 一个工程都没有时显式失败。
//
// 静默返回 ErrNotFound 会把「读不到工程表」伪装成「块不存在」—— 那正是本批要消灭的
// fail-silent。这里用 TRUNCATE projects CASCADE 清空工程表模拟这种环境。
func TestRLS_BlockLocate_EmptyProjectTableFailsExplicitly(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := blockservice.NewService(blockmodel.NewBlockModel(db),
		projectservice.NewService(projectmodel.NewProjectModel(db)))

	_, err := svc.Update(context.Background(), &blockdto.UpdateReq{ID: uuid.NewString(), Name: "x"})
	if !errors.Is(err, blockservice.ErrProjectRequired) {
		t.Fatalf("无工程时定位应显式失败 ErrProjectRequired（而不是静默 ErrNotFound），实际 %v", err)
	}
}

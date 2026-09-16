package rlstest

// rls_inventory_scope_test.go — product/inventory 批次的工程作用域护栏（DB-009 最后一批）。
//
// 这一批补的是 inventory 域**既不在前几批允许面里、也没被任何代理接手**的 7 处：
// 流水列表 / 流水计数 / 库存列表 / BOM 读（单个父 SKU / 批量父 SKU / 父链）/ BOM 全量替换。
// 三张表（inventory_stock_movements、inventory_stocks、inventory_bom_items）都在迁移 215
// 名单里 —— 流水还是分区表（策略装在父表、分区由 215 单独装，见 partition.EnsureAhead）。
//
// 失效形态**按危害排序，不能只看读路径**：
//
//  1. **BOM 成环检测读到空父链 ⇒ 放行成环**（本批最危险的一条）。检测的方向是「从父 SKU
//     沿父链上行，撞到新子项即判环」；读不到 parents 等于上行路径为空，A→B→A 被判成无环
//     并**真的写进库**，之后每次扣减展开都要撞 maxBOMDepth 才认输。所以这里的断言不止
//     「返回了错误」，还必须看**库里有没有那条成环边**；
//  2. **清空 BOM 静默影响 0 行**（ReplaceBOM 的空 rows 分支）：接口回报成功、清单里
//     什么都没删，而清空前后的响应完全一样；
//  3. 流水列表 / 库存列表静默空集、CountMovements 恒 0：列表页显示「暂无数据」而不是报错。
//
// 全程跑在**非超级角色**下（rlsFixture 保证，并用 rls.BypassedRole 自检），
// 否则超级用户无条件绕过 RLS，断言全绿而无意义。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"
)

// invScope 一套「非超级角色 + 两个工程 × (仓库 + 父/子变体 + 各自库存行)」的环境。
type invScope struct {
	db  *gorm.DB
	im  *inventorymodel.Model
	svc *inventoryservice.Service

	pA, pB string

	whA, whB       string
	parentA, compA string
	parentB, compB string
}

// newInvScope 造 fixture（两工程的仓库 / 变体 / 库存行分属各自工程，互不可见）。
func newInvScope(t *testing.T) *invScope {
	t.Helper()
	db, _ := rlsFixture(t)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	im := inventorymodel.NewModel(db)
	svc := inventoryservice.NewService(im, projectservice.NewService(projectmodel.NewProjectModel(db)))

	f := &invScope{db: db, im: im, svc: svc, pA: pA, pB: pB}
	f.whA, f.parentA, f.compA = seedInvProject(t, db, pA, "A")
	f.whB, f.parentB, f.compB = seedInvProject(t, db, pB, "B")
	return f
}

// seedInvProject 在工程下落一行仓库 + 父/子两个变体（各自带一条 10 件的库存真源）。
func seedInvProject(t *testing.T, db *gorm.DB, projectID, sfx string) (warehouseID, parentVariantID, componentVariantID string) {
	t.Helper()
	warehouseID = seedWarehouse(t, db, projectID, "WH"+sfx)
	parentProductID, parentVariantID := seedProductWithVariant(t, db, projectID, "父件 "+sfx)
	componentProductID, componentVariantID := seedProductWithVariant(t, db, projectID, "子件 "+sfx)
	seedStockQty(t, db, projectID, parentProductID, parentVariantID, warehouseID, 10)
	seedStockQty(t, db, projectID, componentProductID, componentVariantID, warehouseID, 10)
	return warehouseID, parentVariantID, componentVariantID
}

// seedStockQty 经 model 写入路径落一行指定数量的库存真源（inventory_stocks 带 FORCE 策略）。
func seedStockQty(t *testing.T, db *gorm.DB, projectID, productID, variantID, warehouseID string, qty int) {
	t.Helper()
	now := time.Now()
	_, err := inventorymodel.NewModel(db).EnsureStock(context.Background(), &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: projectID, WarehouseID: warehouseID,
		ProductID: productID, VariantID: variantID, SKUCode: "SKU-" + variantID[:8],
		Quantity: qty, Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入库存行失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
}

// seedBuiltinReason 经 model 写入路径落一条**内置**变动原因（project_id 为 NULL）。
//
// 215 对 inventory_change_reasons 用的是 global 谓词（放行 project_id IS NULL 的全局行），
// 内置条目即使不设作用域也能写 —— 与迁移 103 的 seed 同形，这里只是把它补进只跑了迁移的库。
func seedBuiltinReason(t *testing.T, db *gorm.DB, code, direction string) {
	t.Helper()
	err := inventorymodel.NewModel(db).CreateReason(context.Background(), &inventorymodel.ReasonEntity{
		ProjectID: nil, Code: code, Name: "护栏原因 " + code, Direction: direction,
		IsBuiltin: true, Status: "active", Sort: 0,
		CreateTime: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("写入内置变动原因失败: %v", err)
	}
}

// seedMovement 经生产写入路径（外层事务 + CreateMovementsTx 自带的 ScopeTx）落一条流水。
func seedMovement(t *testing.T, db *gorm.DB, projectID, warehouseID, variantID, direction string) {
	t.Helper()
	im := inventorymodel.NewModel(db)
	now := time.Now().UTC()
	id := uuid.NewString()
	err := im.Transaction(context.Background(), func(tx *gorm.DB) error {
		return im.CreateMovementsTx(context.Background(), tx, []*inventorymodel.MovementEntity{{
			ID: id, ProjectID: projectID, WarehouseID: warehouseID,
			ProductID: uuid.NewString(), VariantID: variantID, SKUCode: "SKU-" + variantID[:8],
			Direction: direction, Quantity: 1, Delta: 1, QuantityBefore: 0, QuantityAfter: 1,
			ReasonCode: "guard_in", SourceType: "guard", SourceRef: id,
			BatchID: id, CreatedAt: now,
		}})
	})
	if err != nil {
		t.Fatalf("写入库存流水失败（inventory_stock_movements 是分区表，scope 设在调用方事务上）: %v", err)
	}
}

// setBOM 经 service 公开入口全量替换清单（components 为空即**清空**）。
func setBOM(t *testing.T, f *invScope, projectID, parentVariantID string, components ...string) {
	t.Helper()
	items := make([]inventorydto.BOMItemReq, 0, len(components))
	for _, c := range components {
		items = append(items, inventorydto.BOMItemReq{ComponentVariantID: c, Quantity: 2})
	}
	if _, err := f.svc.SetBOM(context.Background(), &inventorydto.SetBOMReq{
		ProjectID: projectID, ParentVariantID: parentVariantID, Items: items,
	}); err != nil {
		t.Fatalf("设置清单失败（projectID=%s parent=%s）: %v", projectID, parentVariantID, err)
	}
}

// bomItems 读某父 SKU 的清单子项（经 model 的作用域路径）。
func bomItems(t *testing.T, f *invScope, projectID, parentVariantID string) []*inventorymodel.BOMItemEntity {
	t.Helper()
	rows, err := f.im.ListBOMItems(context.Background(), parentVariantID, projectID)
	if err != nil {
		t.Fatalf("读清单失败（projectID=%s）: %v", projectID, err)
	}
	return rows
}

// stockQtyScoped 经作用域读某库存行的数量。
//
// 必须包 scope：非超级角色下裸查恒 0 行（fail closed 不报错），
// 直接 Scan 到 int 会得到 0 —— 那是「查不到」而不是「数量是 0」，两者会被混淆。
func stockQtyScoped(t *testing.T, f *invScope, projectID, variantID, warehouseID string) int {
	t.Helper()
	var qty int
	err := rls.InProjectScope(context.Background(), f.db, projectID, func(tx *gorm.DB) error {
		return tx.Raw("SELECT quantity FROM inventory_stocks WHERE variant_id = ? AND warehouse_id = ?",
			variantID, warehouseID).Scan(&qty).Error
	})
	if err != nil {
		t.Fatalf("读库存数量失败: %v", err)
	}
	return qty
}

// TestRLS_InventoryScope_BOMCycleDetectionStaysInProject 成环检测必须真正读到父链。
//
// 这是本批最危险的一条：inventory_bom_items 带 FORCE 策略，ListBOMParents 若拿不到
// 工程作用域就读到**空父链**，而空父链在成环判定里等价于「上行一路没撞到新子项」——
// 判定返回「无环」并**放行写入**。它的后果不是「查不到数据」，而是把该拒绝的清单写进库，
// 之后每次扣减展开都要递归到 maxBOMDepth 才认输。
//
// 断言三层，缺一层都可能放过退化：
//   - 本工程读得到父链（判定才有依据）；
//   - 他工程的作用域读同一列 0 行（这就是「漏包 scope 后判定看到的东西」）；
//   - 端到端：成环清单被拒 **且库里没有留下那条边**。
func TestRLS_InventoryScope_BOMCycleDetectionStaysInProject(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	// 合法清单：P → C。
	setBOM(t, f, f.pA, f.parentA, f.compA)

	// 端到端（危害最直接的一条，故放最前）：再加 C → P 会成环，必须被拒。
	_, err := f.svc.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.pA, ParentVariantID: f.compA,
		Items: []inventorydto.BOMItemReq{{ComponentVariantID: f.parentA, Quantity: 1}},
	})
	if err == nil || err.Error() != inventoryenums.ErrBOMCycle {
		t.Fatalf("成环清单必须返回 %s，实际 err=%v —— 读到空父链时它会返回 nil 并把环写进库",
			inventoryenums.ErrBOMCycle, err)
	}
	// 第二重证据：库里不该留下那条成环边（只看返回值会漏掉「先写后报错」的实现）。
	if cycleRows := bomItems(t, f, f.pA, f.compA); len(cycleRows) != 0 {
		t.Fatalf("成环边 C→P 不该落库，实际 %d 行 —— 成环被静默放行", len(cycleRows))
	}
	// 反向对照：合法的那条仍在（证明上面的「0 行」不是清单根本没写进去）。
	if n := len(bomItems(t, f, f.pA, f.parentA)); n != 1 {
		t.Fatalf("合法清单 P→C 应仍在，实际 %d 行", n)
	}

	// 机理：上面的判定靠的就是这条父链查询读得到（拿不到作用域时它返回空父链，
	// 而空父链在成环判定里等价于「上行一路没撞到新子项」）。
	parents, err := f.im.ListBOMParents(ctx, f.compA, f.pA)
	if err != nil {
		t.Fatalf("读父链失败: %v", err)
	}
	if len(parents) != 1 || parents[0].ParentVariantID != f.parentA {
		t.Fatalf("工程 A 应看到子件 C 的父是 P，实际 %+v", parents)
	}

	// 拿 B 的作用域读同一列：0 行 —— 这正是漏包 scope 时成环检测拿到的东西。
	cross, err := f.im.ListBOMParents(ctx, f.compA, f.pB)
	if err != nil {
		t.Fatalf("跨工程读父链失败: %v", err)
	}
	if len(cross) != 0 {
		t.Errorf("拿 B 的作用域读 A 的父链应 0 行（行不可见），实际 %d 行", len(cross))
	}
}

// TestRLS_InventoryScope_ClearBOMActuallyDeletes 清空 BOM 必须真的删掉行。
//
// ReplaceBOM 的「rows 为空」分支此前是**裸事务**（签名里拿不到工程 id）：换非超级角色后
// 那条 DELETE 静默匹配 0 行 —— 接口回报成功（SetBOM 末尾还会回读一次并返回空 items），
// 而清单里什么都没删。断言因此必须落到**库里的行数**上，不能只看调用没报错。
func TestRLS_InventoryScope_ClearBOMActuallyDeletes(t *testing.T) {
	f := newInvScope(t)

	setBOM(t, f, f.pA, f.parentA, f.compA)
	if n := len(bomItems(t, f, f.pA, f.parentA)); n != 1 {
		t.Fatalf("准备阶段：清单应为 1 条，实际 %d 条", n)
	}

	// 空 items = 清空。
	setBOM(t, f, f.pA, f.parentA)

	if n := len(bomItems(t, f, f.pA, f.parentA)); n != 0 {
		t.Fatalf("清空后清单应为 0 条，实际 %d 条 —— 清空静默影响 0 行", n)
	}

	// 经作用域直查表本身（与上面的读路径是两条独立证据）。
	var raw int64
	if err := rls.InProjectScope(context.Background(), f.db, f.pA, func(tx *gorm.DB) error {
		return tx.Table("inventory_bom_items").Where("parent_variant_id = ?", f.parentA).Count(&raw).Error
	}); err != nil {
		t.Fatalf("直查清单表失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("清空之后 inventory_bom_items 里不应再有该父 SKU 的行，实际 %d 行", raw)
	}
}

// TestRLS_InventoryScope_ClearBOMStaysInProject 清空只动本工程的行。
//
// 拿别的工程的作用域去清空：不报错，但一行都不该动。这条钉的是「作用域取自谁」——
// 若作用域被写成「父 SKU 的归属」或「不过滤」，跨工程清空就会真的删掉别人的清单。
func TestRLS_InventoryScope_ClearBOMStaysInProject(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	setBOM(t, f, f.pA, f.parentA, f.compA)

	// 拿工程 B 的作用域清空工程 A 的父 SKU 清单。
	if _, err := f.svc.SetBOM(ctx, &inventorydto.SetBOMReq{
		ProjectID: f.pB, ParentVariantID: f.parentA,
	}); err != nil {
		t.Fatalf("跨工程清空不该报错（策略是「不可见」而非「拒绝」），实际 %v", err)
	}

	// A 的清单必须原样还在。
	if n := len(bomItems(t, f, f.pA, f.parentA)); n != 1 {
		t.Fatalf("拿 B 的作用域清空 A 的清单：A 的行必须仍在，实际 %d 条", n)
	}
}

// TestRLS_InventoryScope_MovementListAndCount 流水列表与计数都落在工程作用域内。
//
// 两条静默形态：ListMovementRows 少一条流水就是「流水页少一行」（看起来像数据没记），
// CountMovements 少算就是分页总量归零。二者缺作用域时都**不报错**，
// 所以断言必须同时钉住「本工程看得到」与「他工程看不到」。
func TestRLS_InventoryScope_MovementListAndCount(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	seedMovement(t, f.db, f.pA, f.whA, f.parentA, inventoryenums.DirectionIn)
	seedMovement(t, f.db, f.pA, f.whA, f.parentA, inventoryenums.DirectionOut)
	seedMovement(t, f.db, f.pB, f.whB, f.parentB, inventoryenums.DirectionIn)

	rowsA, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: f.pA}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 A 的流水失败: %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("工程 A 应有 2 条流水，实际 %d 条 —— 缺作用域时这里是静默空集", len(rowsA))
	}
	for _, r := range rowsA {
		if r.ProjectID != f.pA {
			t.Errorf("工程 A 的列表串进了他工程的行: %+v", r)
		}
		// join 来的仓库信息也在作用域内：仓库表同样带策略，Scope 失效时 join 会把整行滤掉。
		if r.WarehouseCode == "" || r.WarehouseName == "" {
			t.Errorf("流水行应带仓库投影（join inventory_warehouses），实际 %+v", r)
		}
	}

	rowsB, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: f.pB}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 B 的流水失败: %v", err)
	}
	if len(rowsB) != 1 || rowsB[0].ProjectID != f.pB {
		t.Fatalf("工程 B 应只见自己的 1 条流水，实际 %+v", rowsB)
	}

	nA, err := f.im.CountMovements(ctx, inventorymodel.MovementFilter{ProjectID: f.pA})
	if err != nil {
		t.Fatalf("数工程 A 的流水失败: %v", err)
	}
	if nA != 2 {
		t.Errorf("工程 A 的流水条数应为 2，实际 %d —— 缺作用域时恒 0", nA)
	}
	nB, err := f.im.CountMovements(ctx, inventorymodel.MovementFilter{ProjectID: f.pB})
	if err != nil {
		t.Fatalf("数工程 B 的流水失败: %v", err)
	}
	if nB != 1 {
		t.Errorf("工程 B 的流水条数应为 1，实际 %d", nB)
	}
}

// TestRLS_InventoryScope_StockRowsList 库存列表落在工程作用域内（含 join 的仓库表）。
func TestRLS_InventoryScope_StockRowsList(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	rowsA, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: f.pA}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 A 的库存列表失败: %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("工程 A 应有 2 行库存（父件 + 子件），实际 %d 行 —— 缺作用域时列表静默空集", len(rowsA))
	}
	for _, r := range rowsA {
		if r.ProjectID != f.pA {
			t.Errorf("工程 A 的列表串进了他工程的行: %+v", r)
		}
		if r.WarehouseCode == "" {
			t.Errorf("库存行应带仓库投影（join inventory_warehouses），实际 %+v", r)
		}
	}

	rowsB, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: f.pB}, 0, 0)
	if err != nil {
		t.Fatalf("读工程 B 的库存列表失败: %v", err)
	}
	if len(rowsB) != 2 {
		t.Fatalf("工程 B 应有自己的 2 行库存，实际 %d 行", len(rowsB))
	}
	for _, r := range rowsB {
		if r.ProjectID != f.pB {
			t.Errorf("工程 B 的列表串进了他工程的行: %+v", r)
		}
	}

	// 按 SKU 收窄时同样受作用域约束（后台库存页走的就是这条）。
	onlyA, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{
		ProjectID: f.pA, VariantID: f.parentA,
	}, 10, 0)
	if err != nil {
		t.Fatalf("按变体过滤读库存失败: %v", err)
	}
	if len(onlyA) != 1 || onlyA[0].VariantID != f.parentA {
		t.Fatalf("按变体过滤应只出 1 行，实际 %+v", onlyA)
	}
}

// TestRLS_InventoryScope_BOMExpansionUsesScope 展开扣减真的按清单走（子项被扣、父项不动）。
//
// ListBOMItemsByParents 缺作用域时每层都读到空子项，有清单的 SKU **全被当成叶子**：
// 扣减变成按父项自身扣，静默少扣子项料、不报错。所以断言落在**两个 SKU 的数量**上，
// 而不是「调用没报错」。
func TestRLS_InventoryScope_BOMExpansionUsesScope(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	setBOM(t, f, f.pA, f.parentA, f.compA) // P → C，用量 2
	seedBuiltinReason(t, f.db, "guard_out", inventoryenums.DirectionOut)

	if _, err := f.svc.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID: f.pA, WarehouseID: f.whA, ReasonCode: "guard_out", ExpandBOM: true,
		Lines: []inventorydto.StockChangeLineReq{{VariantID: f.parentA, Quantity: 3}},
	}); err != nil {
		t.Fatalf("按清单展开扣减失败: %v", err)
	}

	if q := stockQtyScoped(t, f, f.pA, f.parentA, f.whA); q != 10 {
		t.Errorf("有清单的父 SKU 不该被自身扣减（应仍为 10），实际 %d —— 展开读到空子项时会变成 7", q)
	}
	if q := stockQtyScoped(t, f, f.pA, f.compA, f.whA); q != 4 {
		t.Errorf("子项应被扣 3×2=6（10 → 4），实际 %d —— 展开读到空子项时这里会仍是 10", q)
	}

	// 批量父查（展开逐层用的就是它）在别工程的作用域下读不到。
	byParents, err := f.im.ListBOMItemsByParents(ctx, []string{f.parentA}, f.pB)
	if err != nil {
		t.Fatalf("跨工程批量读清单子项失败: %v", err)
	}
	if len(byParents) != 0 {
		t.Errorf("拿 B 的作用域批量读 A 的清单子项应 0 行，实际 %d 行", len(byParents))
	}
	own, err := f.im.ListBOMItemsByParents(ctx, []string{f.parentA}, f.pA)
	if err != nil {
		t.Fatalf("批量读本工程清单子项失败: %v", err)
	}
	if len(own) != 1 {
		t.Errorf("本工程批量读清单子项应得 1 行，实际 %d 行", len(own))
	}
}

// TestRLS_InventoryScope_RejectsMissingProjectID 缺工程作用域时这批入口显式报错。
//
// 与「裸查 0 行」是两种失效形态，都要钉住：裸句柄是**静默** fail closed，而带
// rls.InProjectScope 的入口缺工程时**当场报错**。后者才是想要的 —— 调用方一眼看出是
// 调用点漏传工程，而不是在生产上排查「库存列表突然空了」。
func TestRLS_InventoryScope_RejectsMissingProjectID(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	for _, bad := range []string{"", "not-a-uuid"} {
		label := bad
		if label == "" {
			label = "空串"
		}
		cases := []struct {
			name string
			run  func() error
		}{
			{"ListStockRows(inventory_stocks)", func() error {
				_, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: bad}, 10, 0)
				return err
			}},
			{"ListMovementRows(inventory_stock_movements)", func() error {
				_, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: bad}, 10, 0)
				return err
			}},
			{"CountMovements(inventory_stock_movements)", func() error {
				_, err := f.im.CountMovements(ctx, inventorymodel.MovementFilter{ProjectID: bad})
				return err
			}},
			{"ListBOMItems(inventory_bom_items)", func() error {
				_, err := f.im.ListBOMItems(ctx, f.parentA, bad)
				return err
			}},
			{"ListBOMItemsByParents(inventory_bom_items)", func() error {
				_, err := f.im.ListBOMItemsByParents(ctx, []string{f.parentA}, bad)
				return err
			}},
			{"ListBOMParents(inventory_bom_items)", func() error {
				_, err := f.im.ListBOMParents(ctx, f.compA, bad)
				return err
			}},
			{"ReplaceBOM(inventory_bom_items)", func() error {
				return f.im.ReplaceBOM(ctx, f.parentA, bad, nil)
			}},
		}
		for _, c := range cases {
			if err := c.run(); err == nil || err.Error() != rls.ErrInvalidProjectID.Error() {
				t.Errorf("工程 id 为%s 时 %s 应返回 rls.ErrInvalidProjectID，实际 %v", label, c.name, err)
			}
		}
	}

	// 对照：把作用域换成 A 自己，同一批调用全部成功 —— 上面的红只来自作用域，不来自数据。
	if _, err := f.im.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: f.pA}, 10, 0); err != nil {
		t.Fatalf("工程 A 读自己的库存列表应成功，实际 %v", err)
	}
	if err := f.im.ReplaceBOM(ctx, f.parentA, f.pA, nil); err != nil {
		t.Fatalf("工程 A 清空自己的清单应成功，实际 %v", err)
	}
}

// TestRLS_InventoryScope_FailClosedWithoutScope 未设作用域时这三张表一行都读不到。
//
// 这条把「漏包 scope 的路径」在换角色后的真实表现钉住 —— 0 行且不报错。
// 三张表各测一次：库存真源（读路径）、流水（分区表，父表策略）、物料清单（成环检测的依据）。
func TestRLS_InventoryScope_FailClosedWithoutScope(t *testing.T) {
	f := newInvScope(t)
	ctx := context.Background()

	setBOM(t, f, f.pA, f.parentA, f.compA)
	seedMovement(t, f.db, f.pA, f.whA, f.parentA, inventoryenums.DirectionIn)

	bare := []struct {
		table string
		query func() (int64, error)
	}{
		{"inventory_stocks", func() (int64, error) {
			var n int64
			err := f.db.Table("inventory_stocks").Where("project_id = ?", f.pA).Count(&n).Error
			return n, err
		}},
		{"inventory_stock_movements", func() (int64, error) {
			var n int64
			err := f.db.Table("inventory_stock_movements").Where("project_id = ?", f.pA).Count(&n).Error
			return n, err
		}},
		{"inventory_bom_items", func() (int64, error) {
			var n int64
			err := f.db.Table("inventory_bom_items").Where("project_id = ?", f.pA).Count(&n).Error
			return n, err
		}},
	}
	for _, c := range bare {
		n, err := c.query()
		if err != nil {
			t.Fatalf("裸查 %s 失败: %v", c.table, err)
		}
		if n != 0 {
			t.Errorf("未设 app.project_id 时 %s 应 0 行可见（fail closed），实际 %d 行", c.table, n)
		}
	}

	// 对照：同一批行经作用域路径读得到 —— 差别只在会话变量，不在 WHERE。
	if n := len(bomItems(t, f, f.pA, f.parentA)); n != 1 {
		t.Fatalf("设了作用域应读得到清单，实际 %d 行", n)
	}
	if rows, err := f.im.ListMovementRows(ctx, inventorymodel.MovementFilter{ProjectID: f.pA}, 0, 0); err != nil || len(rows) != 1 {
		t.Fatalf("设了作用域应读得到流水，实际 n=%d err=%v", len(rows), err)
	}

	// 作用域事务结束后变量已还原：再次裸查仍 0 行（会话变量不许泄漏到池里的下一条语句）。
	var raw int64
	if err := f.db.Table("inventory_bom_items").Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("作用域事务结束后应重新 fail closed（0 行），实际 %d 行 —— 会话变量泄漏", raw)
	}
}

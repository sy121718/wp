package productservice

// product_variant_stock_rows.go — 变体清单的「仓库明细」读写两侧（B8）。
//
// 命名别与 product_stock.go 混：那份回答「这个商品在哪些仓有多少」（商品级三态），
// 这份回答「这一行变体引用了哪几条货行、各自当前在库多少、这次要校准成多少」——
// 编辑表单逐行编排的那一层。
//
// 数据来源只有两处，都不新查表：
//   · 归属关系在 product_variants.warehouse_skus（迁移 637，变体自己那一列）；
//   · 每条的行事实在库存契约的 StockFactsByStockIDs（名称 / 图 / 在库 / 是否跟踪）。
// 一次批量取（不是逐变体查）：一屏就是「变体数 × 每变体几条货行」，逐条查会变成几十次往返。

import (
	"context"
	"sort"
	"strings"
	"time"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"

	"gorm.io/gorm"
)

// fillVariantListStocks 给变体清单行填上按仓分组的仓库明细（就地写）。
//
// 失败策略与 fillVariantStock 同一档：库存读不到**不让整页失败**，行上的明细留空。
// 理由一致 —— 明细是展示与校准的入口，不是商品的组成部分。
//
// 但「留空」有两种意思，必须分开：一次读失败若被当成「这个变体没挂任何货」，
// 用户随手一保存就把它原有的关联裁掉了。所以每一行都带上 StocksKnown ——
// 它明确回答「明细读到了没有」，模板据此决定这一行的明细要不要参与提交
// （见 admin/product/variant_stock_panel.html 与 ui/product_variant_write.js）。
func (s *Service) fillVariantListStocks(ctx context.Context, projectID string, rows []productdto.VariantListRowResp) {
	if s.inv == nil || len(rows) == 0 {
		return
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		if rows[i].ID != "" {
			ids = append(ids, rows[i].ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	entities, err := s.m.ListVariantsByIDs(ctx, ids, projectID)
	if err != nil {
		return
	}
	// 变体 → 引用清单（顺序按 JSON 里的记录顺序，同仓多条保持建立时的先后）。
	refsByVariant := make(map[string][]productmodel.WarehouseSKURef, len(entities))
	stockIDs := make([]string, 0, len(entities))
	seen := make(map[string]bool, len(entities))
	for _, e := range entities {
		refs := productmodel.DecodeWarehouseSKUs(e.WarehouseSKUs)
		refsByVariant[e.ID] = refs
		for _, r := range refs {
			if !seen[r.StockID] {
				seen[r.StockID] = true
				stockIDs = append(stockIDs, r.StockID)
			}
		}
	}
	byStock := make(map[string]*inventorycontract.StockFact, len(stockIDs))
	if len(stockIDs) > 0 {
		facts, ferr := s.inv.StockFactsByStockIDs(ctx, projectID, stockIDs)
		if ferr != nil {
			return
		}
		for _, f := range facts {
			if f != nil {
				byStock[f.StockID] = f
			}
		}
	}
	for i := range rows {
		rows[i].Stocks = groupVariantStockRows(refsByVariant[rows[i].ID], byStock)
		rows[i].StocksKnown = true
	}
}

// groupVariantStockRows 引用清单 + 行事实 → 按仓分组的目标行（分组顺序 = 首次出现顺序）。
//
// 悬空引用（货行已被删、facts 里没有）**跳过**：它对运营没有可操作的内容，
// 而留着一条「名字空、数量 0」的行会让人以为那里真有一条货。
func groupVariantStockRows(refs []productmodel.WarehouseSKURef,
	byStock map[string]*inventorycontract.StockFact) []productdto.VariantStockGroupResp {
	groups := make([]productdto.VariantStockGroupResp, 0, 2)
	index := make(map[string]int, 2)
	for _, r := range refs {
		f := byStock[r.StockID]
		if f == nil {
			continue
		}
		at, ok := index[f.WarehouseID]
		if !ok {
			at = len(groups)
			index[f.WarehouseID] = at
			groups = append(groups, productdto.VariantStockGroupResp{
				WarehouseID:   f.WarehouseID,
				WarehouseCode: f.WarehouseCode,
				WarehouseName: f.WarehouseName,
			})
		}
		groups[at].Rows = append(groups[at].Rows, productdto.VariantStockRowResp{
			WarehouseID:   f.WarehouseID,
			WarehouseCode: f.WarehouseCode,
			WarehouseName: f.WarehouseName,
			StockID:       f.StockID,
			SKUCode:       f.SKUCode,
			Quantity:      f.Quantity,
			TrackQuantity: f.TrackQuantity,
			Name:          f.Name,
			ImageURL:      f.ImageURL,
		})
	}
	return groups
}

// variantStockApply 一个变体本次要落的仓库明细。
//
// Prune 决定「提交的清单是不是权威清单」：
//   - 既有变体：是。表单把它的全部明细都渲染出来了，缺哪条就是删了哪条 ——
//     不裁的话「删了这一行」保存之后又回来，运营只能反复删。
//   - 新增变体：不是。它的归属仓行是服务端在保存时才建的，表单里根本不存在那一行，
//     拿提交清单当权威会把它立刻裁掉（结果是「新建的变体没有库存行」）。
type variantStockApply struct {
	VariantID string
	Prune     bool
	Rows      []productdto.VariantStockRowResp
}

// planVariantStockApply 把「清单行上的仓库明细」整理成事务内依次执行的落库清单。
//
// 两件事只在这里做，且都在**写之前**完成（拒绝时一笔都没写）：
//
//  1. **判重**：同一个仓同一个 SKU 在库里只有一条货行（迁移 244 的
//     UNIQUE (warehouse_id, sku_code)）。同一变体里重复出现 = 两条目标落到同一行
//     （后一条覆盖前一条），用户看到的是「我填了两个数」；跨变体重复且数量不一致 =
//     两个变体互相覆盖。两种都拒绝，并指向「合并成一行」。
//  2. **固定顺序**：map 遍历无序，而库存侧按 (仓, 码) 升序加锁；按变体 id 排序，
//     让同一批请求每次都以同一顺序进入加锁区（两个并发请求互相错序就是死锁）。
func planVariantStockApply(byVariant map[string][]productdto.VariantStockRowResp,
	prune map[string]bool) (plan []variantStockApply, err error) {
	if len(byVariant) == 0 {
		return nil, nil
	}
	variantIDs := make([]string, 0, len(byVariant))
	for id := range byVariant {
		variantIDs = append(variantIDs, id)
	}
	sort.Strings(variantIDs)
	// 判重键 (仓, 裸码) → 本次已登记的目标数量（跨变体共用一行时只落一次）。
	planned := make(map[string]int, len(variantIDs))
	plan = make([]variantStockApply, 0, len(variantIDs))
	for _, variantID := range variantIDs {
		rows := make([]productdto.VariantStockRowResp, 0, len(byVariant[variantID]))
		seen := make(map[string]bool, len(byVariant[variantID]))
		for _, r := range byVariant[variantID] {
			key := stockRowKey(r)
			if key == "" {
				continue
			}
			if r.Quantity < 0 {
				// 数量是目标值的绝对值，负数没有含义（与库存侧 adjust 的判据同一条）。
				return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
			}
			if seen[key] {
				return nil, productenums.Err(productenums.ErrVariantStockDuplicated)
			}
			seen[key] = true
			if qty, ok := planned[key]; ok {
				if qty != r.Quantity {
					return nil, productenums.Err(productenums.ErrVariantStockDuplicated)
				}
				continue
			}
			planned[key] = r.Quantity
			rows = append(rows, r)
		}
		plan = append(plan, variantStockApply{VariantID: variantID, Prune: prune[variantID], Rows: rows})
	}
	return plan, nil
}

// stockRowKey 一条明细的判重键：按 (仓, 裸码)。
//
// 不用 stockId 优先：库存侧定行也是按 (仓, 码)，判重键与它不一致时，两条「id 不同但
// 仓码相同」的行会绕过判重、落到同一行上 —— 那正是这条判重要挡的账差。
func stockRowKey(r productdto.VariantStockRowResp) string {
	warehouseID := strings.TrimSpace(r.WarehouseID)
	sku := strings.TrimSpace(r.SKUCode)
	if warehouseID == "" || sku == "" {
		return ""
	}
	return warehouseID + "|" + sku
}

// applyVariantStockRowsTx 落一个变体的仓库明细（事务透传）：数量校准 + 关联清单。
//
// 两件事在一处，因为它们的**判据是同一份提交**：
//  1. 数量（库存契约的 CalibrateVariantStocksTx → adjust 变动）：填了数字就是目标值。
//     **不直写 inventory_stocks** —— 加锁序、流水、变体引用登记三件事都在库存域。
//  2. 关联（product_variants.warehouse_skus）：提交了哪些货行就挂哪些；Prune 为真时
//     表单里没有的那些从清单里去掉。不新建关联表 —— 关联已经由这一列承担。
//
// **顺序是「先校准、后挂引用」，不能反过来**。先 attach 会先用 quantity=nil 把该仓的
// 货行建成「不跟踪（无限）」，紧接着校准就撞上「不跟踪行不许带数量」而被自己拒绝 ——
// 一条本来该成立的第一次入库，变成一次报错。先校准则：行不存在时由变动路径建行
// （track=true + 目标值 + 一条 adjust 流水，见 inventory_change.go ③ 段），行已存在时
// 按目标值调整；随后 attach 幂等地找到那一行、只补引用，不再碰数量。
//
// 未注入库存端口 = 降级（纯商品单测路径）：不做任何事，也不报错 —— 与 ensureVariantStockTx 同档。
func (s *Service) applyVariantStockRowsTx(ctx context.Context, tx *gorm.DB, projectID, productID string,
	step variantStockApply) error {
	if s.invSvc == nil || strings.TrimSpace(step.VariantID) == "" {
		return nil
	}
	v, gerr := s.m.GetVariantScopedTx(ctx, tx, step.VariantID, projectID)
	if gerr != nil {
		return mapNotFound(gerr)
	}
	type resolvedRow struct {
		ref *productcontract.WarehouseRef
		sku string
	}
	resolved := make([]resolvedRow, 0, len(step.Rows))
	targets := make([]inventorycontract.ProductStockTarget, 0, len(step.Rows))
	for _, r := range step.Rows {
		warehouseID := strings.TrimSpace(r.WarehouseID)
		if warehouseID == "" {
			return productenums.Err(productenums.ErrInvalidParam)
		}
		ref, rerr := s.invSvc.ResolveWarehouse(ctx, projectID, warehouseID)
		if rerr != nil {
			return rerr
		}
		if ref == nil {
			return productenums.Err(productenums.ErrInvalidParam)
		}
		sku := strings.TrimSpace(r.SKUCode)
		if sku == "" {
			return productenums.Err(productenums.ErrVariantSKUEmpty)
		}
		// 归一成**裸码**：仓库里的行永远叫裸码，前缀只属于商品侧。前端读回来的是裸码，
		// 但导入 / 手工构造的请求可能带前缀 —— 剥一次是幂等的（与 calibrateLine 同口径）。
		sku = stripWarehousePrefix(sku, ref.Code)
		resolved = append(resolved, resolvedRow{ref: ref, sku: sku})
		targets = append(targets, inventorycontract.ProductStockTarget{
			StockID: r.StockID, WarehouseID: ref.ID, SKUCode: sku, Quantity: r.Quantity,
		})
	}
	// ① 数量校准（一次 adjust 变动：一个变体的这批货是同一个批次，流水里能对账）。
	if len(targets) > 0 {
		if cerr := s.invSvc.CalibrateVariantStocksTx(ctx, tx, projectID, productID, step.VariantID, targets); cerr != nil {
			return cerr
		}
	}
	// ② 引用登记：校准走过来的行已被变动路径登记过（641 的 ②.5 段），这里对它们
	//    是幂等的空转；真正的活是「目标值 0 且行还不存在」那种没被校准碰到的行。
	submitted := make(map[string]bool, len(resolved))
	for _, r := range resolved {
		stockID, aerr := s.attachWarehouseStockTx(ctx, tx, r.ref, v, r.sku, "", nil)
		if aerr != nil {
			return aerr
		}
		if stockID != "" {
			submitted[stockID] = true
		}
	}
	if !step.Prune {
		return nil
	}
	return s.pruneVariantStockRefsTx(ctx, tx, projectID, v, submitted)
}

// pruneVariantStockRefsTx 把变体引用清单裁到「本次提交的那些货行」（Prune 语义见 variantStockApply）。
//
// submitted 为空也照裁：那表示「这个变体的货行全被删了」，清单应当变成空数组。
// 不写 null —— EncodeWarehouseSKUs 对 nil 输出 "[]"（列是 not null，读到 null 会炸）。
func (s *Service) pruneVariantStockRefsTx(ctx context.Context, tx *gorm.DB, projectID string,
	v *productmodel.VariantEntity, submitted map[string]bool) error {
	refs := productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs)
	kept := make([]productmodel.WarehouseSKURef, 0, len(refs))
	for _, r := range refs {
		if submitted[r.StockID] {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(refs) {
		// 一条都没裁：不写（省一次 UPDATE，也让「只改数量」这条路径不碰这一列）。
		return nil
	}
	raw := productmodel.EncodeWarehouseSKUs(kept)
	if err := s.m.UpdateVariantWarehouseSKUsTx(ctx, tx, v.ID, raw, time.Now().UTC()); err != nil {
		return err
	}
	v.WarehouseSKUs = raw
	return nil
}

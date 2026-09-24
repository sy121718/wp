// inventory_bom.go — 物料清单（issue #16 验收 5）。
//
// 物料清单是「父 SKU → 子项 SKU × 用量」的派生物：扣减父 SKU 时把它展开成
// 若干子项 SKU，各子项各自的真源行上加锁扣减（整体一个事务、整体成功或整体拒绝）。
//
// 三条规则：
//
//	· 全量替换而非追加：同一父 SKU 的清单一次给全，先删后写（聚合内原子组合）；
//	· 空清单 = 清空：之后扣减该 SKU 就按它自己扣；
//	· 成环在维护入口就拒绝（A→B→A 会让展开无限递归），展开时另设层数上限兜底。
package inventoryservice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

// SetBOM 全量替换某个父 SKU 的物料清单（验收 5）。
func (s *Service) SetBOM(ctx context.Context, req *inventorydto.SetBOMReq) (res *inventorydto.BOMResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	parentVariantID := strings.TrimSpace(req.ParentVariantID)
	if parentVariantID == "" {
		return nil, errors.New(inventoryenums.ErrStockVariantRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	seen := make(map[string]bool, len(req.Items))
	rows := make([]*inventorymodel.BOMItemEntity, 0, len(req.Items))
	for _, item := range req.Items {
		componentID := strings.TrimSpace(item.ComponentVariantID)
		if componentID == "" {
			return nil, errors.New(inventoryenums.ErrBOMComponentRequired)
		}
		if componentID == parentVariantID {
			return nil, errors.New(inventoryenums.ErrBOMSelfReference)
		}
		if item.Quantity <= 0 {
			return nil, errors.New(inventoryenums.ErrBOMQuantityInvalid)
		}
		if seen[componentID] {
			return nil, errors.New(inventoryenums.ErrBOMDuplicateComponent)
		}
		seen[componentID] = true
		rows = append(rows, &inventorymodel.BOMItemEntity{
			ID: uuid.NewString(), ProjectID: projectID,
			ParentVariantID: parentVariantID, ParentSKUCode: strings.TrimSpace(req.ParentSKUCode),
			ComponentVariantID: componentID, ComponentSKUCode: strings.TrimSpace(item.ComponentSKUCode),
			Quantity: item.Quantity, CreatedAt: now, UpdatedAt: now,
		})
	}
	// 成环检测与全量替换都在工程作用域内（inventory_bom_items 在迁移 215 名单里）：
	// 检测读不到父链会**放行成环**，替换缺作用域则静默 0 行。两处都用调用方已解析出的
	// projectID，不依赖「唯一工程兜底」——那在多工程下会退化成 ErrInvalidParam。
	if err = s.assertBOMNoCycle(ctx, parentVariantID, projectID, rows); err != nil {
		return nil, err
	}
	if err = s.m.ReplaceBOM(ctx, parentVariantID, projectID, rows); err != nil {
		return nil, err
	}
	return s.GetBOM(ctx, &inventorydto.GetBOMReq{ProjectID: projectID, ParentVariantID: parentVariantID})
}

// GetBOM 某个父 SKU 的物料清单（无清单返回空 items，不是错误）。
func (s *Service) GetBOM(ctx context.Context, req *inventorydto.GetBOMReq) (res *inventorydto.BOMResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	parentVariantID := strings.TrimSpace(req.ParentVariantID)
	if parentVariantID == "" {
		return nil, errors.New(inventoryenums.ErrStockVariantRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListBOMItems(ctx, parentVariantID, projectID)
	if err != nil {
		return nil, err
	}
	res = &inventorydto.BOMResp{
		ParentVariantID: parentVariantID,
		Items:           make([]*inventorydto.BOMItemResp, 0, len(rows)),
	}
	for _, r := range rows {
		res.ProjectID = r.ProjectID
		res.ParentSKUCode = r.ParentSKUCode
		res.UpdatedAt = r.UpdatedAt.Format(time.RFC3339)
		res.Items = append(res.Items, &inventorydto.BOMItemResp{
			ID: r.ID, ComponentVariantID: r.ComponentVariantID,
			ComponentSKUCode: r.ComponentSKUCode, Quantity: r.Quantity,
		})
	}
	return res, nil
}

// assertBOMNoCycle 成环检测（作用域由调用方给）：新增 P → C 会成环，当且仅当「C 已经能走到 P」
// （即 P 是 C 的祖先）。判定办法是从 P 沿「谁把 X 当子项」一路上行，
// 若途中撞到任何一个新子项 C，就说明存在 C → … → P 的既有路径，加上 P → C 即成环。
//
// 方向很容易写反：从子项向上找到的是「C 的祖先」，那只能说明 P → … → C 早已存在，
// 再加 P → C 只是多了一条通路，并不成环 —— 这样的清单会被放行，展开时直接死循环。
//
// projectID 必须传：inventory_bom_items 带 FORCE 策略，缺作用域时 ListBOMParents 读到空
// 父链 —— 判定于是「一路都没撞到新子项」，**成环被静默放行**。这不是查不到数据，
// 而是把该拒绝的写入放行（清单已经落库，靠下游 maxBOMDepth 才表现出来）。
func (s *Service) assertBOMNoCycle(ctx context.Context, parentVariantID, projectID string,
	rows []*inventorymodel.BOMItemEntity) (err error) {
	components := make(map[string]bool, len(rows))
	for _, r := range rows {
		components[r.ComponentVariantID] = true
	}
	visited := make(map[string]bool, len(rows)+1)
	stack := []string{parentVariantID}
	for len(stack) > 0 {
		current := stack
		stack = nil
		for _, id := range current {
			if visited[id] {
				continue
			}
			visited[id] = true
			parents, perr := s.m.ListBOMParents(ctx, id, projectID)
			if perr != nil {
				return perr
			}
			for _, p := range parents {
				if components[p.ParentVariantID] {
					return errors.New(inventoryenums.ErrBOMCycle)
				}
				stack = append(stack, p.ParentVariantID)
			}
		}
	}
	return nil
}

// expandBOM 按物料清单把「父 SKU 变动项」展开成「叶子 SKU 变动项」（验收 5）。
//
// 逐层展开（BFS）：有清单的 SKU 换成子项（用量 = 子项用量 × 请求量），
// 没有清单的 SKU 就是叶子，按自身扣减。子项沿用**父项解析出的仓库**，
// 这样一次扣减的仓库口径唯一；祖先链随节点携带，出现环即拒绝。
//
// projectID 是 inventory_bom_items 的作用域（迁移 215）：缺它时每层都读到空子项，
// 有清单的 SKU 全被当成叶子 —— 扣减静默少扣子项料，且不报错。
func (s *Service) expandBOM(ctx context.Context, projectID string, items []changeItem) (out []changeItem, err error) {
	type node struct {
		item  changeItem
		chain map[string]bool
	}
	frontier := make([]node, 0, len(items))
	for _, it := range items {
		frontier = append(frontier, node{item: it, chain: map[string]bool{it.key.variantID: true}})
	}
	out = make([]changeItem, 0, len(items))
	for depth := 0; depth < maxBOMDepth; depth++ {
		if len(frontier) == 0 {
			return out, nil
		}
		parents := make([]string, 0, len(frontier))
		for _, n := range frontier {
			parents = append(parents, n.item.key.variantID)
		}
		rows, lerr := s.m.ListBOMItemsByParents(ctx, parents, projectID)
		if lerr != nil {
			return nil, lerr
		}
		children := make(map[string][]*inventorymodel.BOMItemEntity, len(rows))
		for _, r := range rows {
			children[r.ParentVariantID] = append(children[r.ParentVariantID], r)
		}
		next := make([]node, 0, len(frontier))
		for _, n := range frontier {
			kids := children[n.item.key.variantID]
			if len(kids) == 0 {
				out = append(out, n.item)
				continue
			}
			for _, kid := range kids {
				if n.chain[kid.ComponentVariantID] {
					return nil, errors.New(inventoryenums.ErrBOMCycle)
				}
				chain := make(map[string]bool, len(n.chain)+1)
				for id := range n.chain {
					chain[id] = true
				}
				chain[kid.ComponentVariantID] = true
				child := n.item
				child.key = stockKey{variantID: kid.ComponentVariantID, warehouseID: n.item.key.warehouseID}
				child.quantity = n.item.quantity * kid.Quantity
				child.skuCode = kid.ComponentSKUCode
				// 子项的商品 id 由真源快照解析（建行时才会用到），不在这里猜。
				child.productID = ""
				child.parentVariantID = n.item.key.variantID
				next = append(next, node{item: child, chain: chain})
			}
		}
		frontier = next
	}
	if len(frontier) > 0 {
		// 列表里不可能有环（维护入口已拒绝），这是数据异常的第二道防线。
		return nil, errors.New(inventoryenums.ErrBOMDepthExceeded)
	}
	return out, nil
}

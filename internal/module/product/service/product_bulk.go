package productservice

// product_bulk.go — 五个批量删除用例的 service 入口（task-156，dd.txt §5.1/§5.3/§5.4）。
//
// 为什么要有这一批用例：控制器此前把「循环 + 累加计数 + 挑文案」都放在 handler 里，
// 于是每个批量操作实际上就是 N 次单条 service 调用，SQL 次数是
// N ×（逐条校验 + 删除 + 失效入队）。这里把 N 压成一个用例：
//   · 校验阶段批量化（一次查回整批：存在性 / 子级 / 引用面）；
//   · 删除阶段一条 DELETE ... WHERE id IN (...)，在**同一个事务**里连同失效事件提交；
//   · 结果用 productdto.BulkResultResp 回给控制器（控制器不再累加任何计数）。
//
// 语义刻意保留的部分（dd.txt §5.3）：
//   · 「逐项成功逐项失败」仍然成立 —— 校验阶段被判掉的项（不存在 / 有子级 / 被引用 /
//     跨工程引用）不进删除集合，其余项照常删除，用户看到的仍是「已删 N / 未成功 M」；
//   · 真正要删的那些项落在**同一个事务**里：删除本身失败（约束冲突这类）时整批回滚并报错，
//     而不是留一半 —— 这一点比改造前更强（改造前是 N 个独立事务，中途失败会留下
//     「删了一半」而提示页只说「未成功 M 个」）；
//   · 批量读取 / 守卫扫描本身失败一律**返回 error**（不再像改造前那样被控制器算进
//     「跳过」的计数里 —— 内部错误与业务拒绝混在一个数字里，用户与运维都读不出区别）。
//
// 事务边界与作用域：每个用例一个事务；分类 / 品牌 / 属性 / 标签都带工程隔离
// （rls.ScopeTx 逐工程设置），标签那条按「标签自己的工程」分组切换（见 DeleteTag 注释）。

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/masterdata/contract"
	"go_wp/internal/module/masterdata/enums"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/module/product/model"
	"go_wp/pkg/facing"
	"go_wp/pkg/rls"
)

// normalizeBulkIDs 归一批量 id 清单：去空白、去空、去重（保序）。
//
// inbound 已经过 shell.BulkIDs（同样的归一 + 上限 200），这里再做一次是为了让用例**自己
// 成立**：直接调 service（测试 / 其它入口）时也不能靠调用方保证「没有重复 id」——
// 重复 id 会让 Requested 虚高，也会让「只该删一次」的操作被算两次。
func normalizeBulkIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// BulkDeleteProducts 批量删除商品（连同其全部变体、各仓库存行 —— 由外键级联）。
//
// 事务语义（改造前 → 改造后）：
//
//	· 改造前：每条商品一个独立事务（product.go Delete：删除 + 失效 + 留痕），
//	  一条失败不影响其它条（控制器把失败算进「跳过」）；
//	· 改造后：校验批量化，**要删的商品在同一个事务里删**（删除 + 逐商品失效 + 整批留痕），
//	  任一步失败整批回滚并报错。「逐项跳过」的语义不变（不存在的项不进删除集合）。
//
// 行锁：删除集合是先「锁定 + 读快照」再删 —— LockProductsTx 按 id 升序 FOR UPDATE，
// 锁住的行就是本次要删的行；锁之后行数对不上（被并发删掉）即整批回滚，不留半截状态。
func (s *Service) BulkDeleteProducts(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids := normalizeBulkIDs(req.IDs)
	res = &productdto.BulkResultResp{}
	if len(ids) == 0 {
		return res, nil
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 留痕快照要在事务前取（删除后就读不到了）：一次查回整批商品与它们的全部变体。
	rows, err := s.m.ListProductsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	variants, err := s.m.ListVariantsByProducts(ctx, ids)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rows))
	for _, e := range rows {
		names[e.ID] = e.Name
	}
	deleted := 0
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		locked, lerr := s.m.LockProductsTx(ctx, tx, ids, projectID)
		if lerr != nil {
			return lerr
		}
		// 锁不到的 = 不存在 / 不属于本工程 / 已被并发删掉：**整批不删**。
		// 逐条提交时这一条会被算进「跳过」，用户拿到的是「成功 49 / 跳过 1」——
		// 他既不知道是哪一条，也没法只重试那一条。
		if len(locked) != len(ids) {
			lockedSet := make(map[string]bool, len(locked))
			for _, id := range locked {
				lockedSet[id] = true
			}
			blocked := make([]productdto.BulkBlockedItemResp, 0, len(ids)-len(locked))
			for _, id := range ids {
				if !lockedSet[id] {
					blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
				}
			}
			return bulkRejected(blocked)
		}
		lockedSet := make(map[string]bool, len(locked))
		for _, id := range locked {
			lockedSet[id] = true
		}
		// 留痕按**锁定到的**商品组装（并发删掉的那些不写留痕 —— 它们不是本次删的）。
		inputs := make([]*masterdatacontract.ChangeInput, 0, len(locked)+len(variants))
		for _, e := range rows {
			if !lockedSet[e.ID] {
				continue
			}
			inputs = append(inputs, productChangeInput(e, masterdataenums.ActionDelete,
				masterdataenums.OriginProduct, "", productChangeSnapshot(e), nil))
		}
		for _, v := range variants {
			if !lockedSet[v.ProductID] {
				continue
			}
			inputs = append(inputs, variantChangeInput(projectID, v, masterdataenums.ActionDelete,
				masterdataenums.OriginVariant, "", variantChangeSnapshot(v, nil), nil))
		}
		n, derr := s.m.DeleteProductsTx(ctx, tx, locked)
		if derr != nil {
			return derr
		}
		if int(n) != len(locked) {
			return fmt.Errorf("product: 批量删除商品的行数对不上：锁定 %d，删除 %d", len(locked), n)
		}
		deleted = len(locked)
		// 删除 = 集合成员变化，逐商品各发一条（enqueueInvalidationTx 内部按实体去重）。
		targets := make([]invalidationTarget, 0, len(locked))
		for _, id := range locked {
			targets = append(targets, productInvalidationTarget(id))
		}
		if xerr := s.enqueueInvalidationTx(ctx, tx, projectID, targets...); xerr != nil {
			return xerr
		}
		return s.recordChangesTx(ctx, tx, inputs...)
	}); err != nil {
		return nil, err
	}
	s.bumpFragmentCache(ctx, projectID)
	// Done 取真正删掉的行数（锁定集合大小，且已校验 == len(ids)）。
	res.Done = deleted
	return res, nil
}

// bulkRejected 把「哪几条不能处理」包成整批拒绝的错误。
//
// 状态码 409（冲突）在**声明处**给：一条也不能处理是「当前数据状态不允许这次操作」，
// 不是参数错（400），也不是内部故障（500）。出口照常读 pkg/facing.StatusOf，不认识这个类型。
func bulkRejected(items []productdto.BulkBlockedItemResp) error {
	return &productdto.BulkRejectedError{
		Err: facing.WithStatus(
			facing.New(productenums.ErrBulkRejected, productenums.ProductErrFallbacks[productenums.ErrBulkRejected]),
			http.StatusConflict),
		Items: items,
	}
}

// bulkBlocked 一条可定位的拒绝原因。名称拿不到时退 id（uuid 至少能让人去搜）。
func bulkBlocked(id, name, reasonKey string) productdto.BulkBlockedItemResp {
	return productdto.BulkBlockedItemResp{ID: id, Name: strings.TrimSpace(name), ReasonKey: reasonKey}
}

// bulkBlockedNames 整批 id 的「名称表」（拿不到名称的条目留空）。
func bulkBlockedNames(ids []string, named map[string]string) map[string]string {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		out[id] = named[id]
	}
	return out
}

// BulkDeleteProductCategories 批量删除分类。
//
// 三条前置规则与单条版（DeleteCategory）逐条一致，只是改成一次算完整批：
//
//	① 不存在 → Skipped；
//	② 仍有子级 → Skipped（外键是 SET NULL，会把子级静默提升为顶级，层级信息就丢了）；
//	③ 仍被**任何工程**的商品引用（附属 category_ids 或主分类 primary_category_id）→ Skipped。
func (s *Service) BulkDeleteProductCategories(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids := normalizeBulkIDs(req.IDs)
	res = &productdto.BulkResultResp{}
	if len(ids) == 0 {
		return res, nil
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListCategoriesByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	childCounts, err := s.m.CountCategoryChildrenByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByCategoryIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		present[r.ID] = true
		names[r.ID] = r.Name
	}
	blocked := make([]productdto.BulkBlockedItemResp, 0)
	for _, id := range ids {
		switch {
		case !present[id]:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case childCounts[id] > 0:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrCategoryHasChildren))
		case hits[id].Referenced():
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrCategoryInUse))
		}
	}
	if len(blocked) > 0 {
		// 任何一条不能删 → **一条都不删**（判据见 internal/module/CLAUDE.md「列表页批量操作」）。
		return nil, bulkRejected(blocked)
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		n, derr := s.m.DeleteCategoriesTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if int(n) != len(ids) {
			return fmt.Errorf("product: 批量删除分类的行数对不上：计划 %d，删除 %d", len(ids), n)
		}
		targets := make([]invalidationTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, invalidationTarget{
				EntityType: productcontract.EntityTypeCategory, EntityID: id,
			})
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
	}); err != nil {
		return nil, err
	}
	res.Done = len(ids)
	return res, nil
}

// BulkDeleteProductBrands 批量删除品牌。
//
// 与单条版（DeleteBrand）一致：被**任何工程**的商品引用即拒绝（外键 ON DELETE SET NULL
// 会静默解绑别的工程那个商品的品牌 —— 没有任何信号，这正是品牌守卫存在的理由）。
func (s *Service) BulkDeleteProductBrands(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids := normalizeBulkIDs(req.IDs)
	res = &productdto.BulkResultResp{}
	if len(ids) == 0 {
		return res, nil
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListBrandsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByBrandIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		present[r.ID] = true
		names[r.ID] = r.Name
	}
	blocked := make([]productdto.BulkBlockedItemResp, 0)
	for _, id := range ids {
		switch {
		case !present[id]:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case hits[id].Referenced():
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrBrandInUse))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		n, derr := s.m.DeleteBrandsTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if int(n) != len(ids) {
			return fmt.Errorf("product: 批量删除品牌的行数对不上：计划 %d，删除 %d", len(ids), n)
		}
		targets := make([]invalidationTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, invalidationTarget{
				EntityType: productcontract.EntityTypeBrand, EntityID: id,
			})
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
	}); err != nil {
		return nil, err
	}
	res.Done = len(ids)
	return res, nil
}

// BulkDeleteProductAttributes 批量删除属性组（属性值由外键级联）。
//
// 与单条版（DeleteAttribute）一致：被任何工程的商品引用即拒绝 —— attribute_ids 是 JSON
// 数组、没有数据库级外键兜底，删掉就会留下悬空引用。
func (s *Service) BulkDeleteProductAttributes(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids := normalizeBulkIDs(req.IDs)
	res = &productdto.BulkResultResp{}
	if len(ids) == 0 {
		return res, nil
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListAttributesByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByAttributeIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		present[r.ID] = true
		names[r.ID] = r.Name
	}
	blocked := make([]productdto.BulkBlockedItemResp, 0)
	for _, id := range ids {
		switch {
		case !present[id]:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case hits[id].Referenced():
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrAttrInUse))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		n, derr := s.m.DeleteAttributesTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if int(n) != len(ids) {
			return fmt.Errorf("product: 批量删除属性组的行数对不上：计划 %d，删除 %d", len(ids), n)
		}
		targets := make([]invalidationTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, attributeInvalidationEntityKey(id))
		}
		return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
	}); err != nil {
		return nil, err
	}
	res.Done = len(ids)
	return res, nil
}

// BulkDeleteProductTags 批量删除标签（手工 / 自动一视同仁）。
//
// 与单条版（DeleteTag）的两条语义逐条一致：
//
//	· **本工程**内的引用不拒绝 —— 解绑即可（标签是标记不是外键），但「解绑 + 删行」必须原子；
//	· **别的工程**的引用一律打回给人（Skipped）：不替别的工程改数据，也不留下永久悬空 id。
//
// 写作用域是**标签自己的工程**（与单条版一致）：分组后逐工程切换 rls 作用域，
// 每组建一次解绑 + 失效入队 + 删行，全部在同一事务里。
func (s *Service) BulkDeleteProductTags(ctx context.Context, req *productdto.BulkDeleteReq) (res *productdto.BulkResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids := normalizeBulkIDs(req.IDs)
	res = &productdto.BulkResultResp{}
	if len(ids) == 0 {
		return res, nil
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListTagsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	hits, err := s.batchRefHits(ctx, ids, func(scope []string) (map[string]*productmodel.BatchRefHit, error) {
		return s.m.ProductRefsByTagIDs(ctx, ids, scope)
	})
	if err != nil {
		return nil, err
	}
	owner := make(map[string]string, len(rows))
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		owner[r.ID] = r.ProjectID
		names[r.ID] = r.Name
	}
	blocked := make([]productdto.BulkBlockedItemResp, 0)
	groups := map[string][]string{}
	for _, id := range ids {
		pid, ok := owner[id]
		switch {
		case !ok:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrNotFound))
		case len(hits[id].Outside(pid)) > 0:
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrTagCrossProject))
		default:
			groups[pid] = append(groups[pid], id)
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	scopes := make([]string, 0, len(groups))
	for pid := range groups {
		scopes = append(scopes, pid)
	}
	sort.Strings(scopes)
	now := time.Now().UTC()
	if len(scopes) > 0 {
		if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			for _, pid := range scopes {
				if serr := rls.ScopeTx(tx, pid); serr != nil {
					return serr
				}
				gids := groups[pid]
				if _, rerr := s.m.RemoveTagsFromProductsTx(tx, gids, pid, now); rerr != nil {
					return rerr
				}
				targets := make([]invalidationTarget, 0, len(gids))
				for _, id := range gids {
					targets = append(targets, invalidationTarget{
						EntityType: productcontract.EntityTypeTag, EntityID: id,
					})
				}
				if xerr := s.enqueueInvalidationTx(ctx, tx, pid, targets...); xerr != nil {
					return xerr
				}
				n, derr := s.m.DeleteTagsTx(ctx, tx, gids)
				if derr != nil {
					return derr
				}
				if int(n) != len(gids) {
					return fmt.Errorf("product: 批量删除标签的行数对不上：计划 %d，删除 %d", len(gids), n)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	res.Done = len(ids)
	return res, nil
}

// batchRefHits 跨工程引用扫描的批量入口：工程清单取一次，交给扫描函数算整批。
//
// 清单拿不到时**报错**（crossProjectRefScope 的既有约定）：空清单会让结论变成
// 「没有任何引用」—— 那正是要消灭的静默放过形态。
func (s *Service) batchRefHits(ctx context.Context, entityIDs []string,
	scan func(projectIDs []string) (map[string]*productmodel.BatchRefHit, error)) (map[string]*productmodel.BatchRefHit, error) {
	scope, err := s.crossProjectRefScope(ctx)
	if err != nil {
		return nil, err
	}
	return scan(scope)
}

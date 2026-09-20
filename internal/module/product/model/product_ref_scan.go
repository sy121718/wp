package productmodel

// product_ref_scan.go — 删除守卫的**跨工程**引用扫描（审计 DB-03 §1.2 / §5.1 第 2 条，PROD-02）。
//
// 守卫要回答的问题是「**全库**还有谁引用它」，而此前的写法把作用域收在「发起删除的那个
// 工程」（rls.InProjectScope(ctx, m.db, projectID, …)，projectID 是删除请求里的工程）。
// 于是另一个工程的商品仍引用着这条分类 / 品牌 / 属性时，守卫命中 0 行 ⇒ 删除放行 ⇒
// products 的 JSONB 数组里留下一辈子不会被清理的悬空 id（审计 §3.2 的实测复现）。
//
// ---------------------------------------------------------------------------
// 跨工程可见性走的是哪条路（任务 PROD-02 要求把连接身份讲清楚，不许假装成立）
// ---------------------------------------------------------------------------
//
// 前提①：当前应用连接是**超级用户**（DB-009；启动探针 database.CheckRLSIdentity 会为此
// 打 WARN），而 PostgreSQL 的超级用户 / BYPASSRLS 角色**无条件绕过** RLS ——
// 迁移 215 给 products 装的 FORCE 策略对它一行都挡不住。所以「不带作用域的一条查询」
// 现在恰好能看见全部工程的行，但那是**角色的偶然属性**，不是可依赖的语义。
//
// 前提②：换成非超级角色（DB-04 已备好 go_wp_app + database.require_rls_role）之后，
// 那条「不带作用域的查询」会 fail closed 返回 0 行 —— 守卫静默放行，正是本次要消灭的
// 失效形态。
//
// 因此本文件的实现**不依赖超级用户**，跨工程可见性由**逐工程作用域枚举**取得：
//
//	for 每个工程 P（清单来自 projects 表 —— 该表不带 project_id、不在迁移 215 名单里，
//	    任何角色都读得到；且 products.project_id 有 REFERENCES projects(id) 外键，
//	    枚举 projects 不会漏掉任何商品）:
//	    事务内 SET LOCAL app.project_id = P（pkg/rls.ScopeTx）
//	    查 WHERE project_id = P AND <引用谓词> 的商品
//	→ 各工程命中取并集
//
// 结论：**换连接角色后本守卫仍然成立**，两种身份下结果一致。理由有三条：
//  1. 查询自带 project_id = P 谓词，命中行属于哪个工程由谓词决定，不由策略是否生效决定；
//  2. 工程枚举来自 projects（无 RLS），不依赖任何被策略保护的表；
//  3. 逐工程设置作用域后，即使策略生效，每个工程的查询都在「该工程可见」的上下文里跑。
//
// 与 DB-05（06970bfc）的关系：那批给 ListCategoriesByIDs / ListTagsByIDs / ListBrandsByIDs /
// ListAttributesByIDs / ListVariantsByIDs 补的工程作用域**原样保留**（本文件一行都没回退它们）——
// 那些是「按 id 批量读本工程数据」的正常路径，作用域正是它们要的语义；本文件是同一口径的
// 延伸（每条查询都有自己的工程作用域），不是回退。
//
// 已知代价与边界：
//   · 一次守卫 = O(工程数) 条查询（每个工程 1 条，命中满额时再补 1 条计数）。删除是低频
//     操作，工程数是站点工程量级；相比之下「静默留下悬空引用」的代价高得多。
//   · 工程清单由 service 经 project 契约 List() 取得；并发新建的工程在清单快照之后不可见 ——
//     但新工程里不可能出现「引用别的工程的分类」：写入路径（resolveCategoryIDs /
//     resolveBrandID / resolveAttributeIDs / 捆绑成员解析）一律拒绝跨工程引用，
//     跨工程引用只可能来自存量或绕过 service 的写入。
//   · 变体（product_variants）自身没有 project_id 列、也没有策略；捆绑成员引用查的是
//     **products.bundle_items**（有策略的表），所以变体守卫同样按工程枚举。
//   · 本文件只做「发现」，不做任何清理：命中即交给 service 打回给人（AGENTS.md
//     「冲突与数据不一致一律打回给人」，禁止自动清理 / 静默放过）。

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// maxRefSamplePerProject 每个工程最多采样的引用方商品数（错误里要列的商品 id 上限）。
//
// 分两段取（采样 + 计数）而不是把命中行全拉进内存：一个分类被几万个商品引用时，
// 「为了报出总数而加载全部行」本身就是新的故障面。采样满额才补一条 COUNT，
// 未满额时行数即总数（常见的 0 命中路径因此只有 1 条查询）。
const maxRefSamplePerProject = 3

// RefProduct 引用方商品（错误里定位用：商品 id + 主体 SKU 编码 + 它属于哪个工程）。
//
// 带上 sku_code 是「哪个码」那一条：运营在后台是按 SKU / 商品编码找东西的，
// 只给 uuid 还得再查一次库（列宽也放不下）。
type RefProduct struct {
	ProductID string
	ProjectID string
	// SKUCode 引用方商品的主体 SKU（存量可能为空串 —— 那时只报 id 与工程）。
	SKUCode string
}

// CrossProjectRef 一次跨工程引用扫描的结论（可定位信息）。
//
// 它不是「有没有被引用」的布尔值，而是「谁在引用」的清单：错误里要报出哪张表 /
// 哪个码 / 涉及几个工程与哪些商品 id，让人决定怎么处置（解绑还是保留）。
type CrossProjectRef struct {
	// RefColumns 命中的引用面（"表.列" 形式，可能多条 —— 分类同时看附属分类与主分类）。
	RefColumns []string
	// ProjectIDs 命中引用的工程（去重、升序）。
	ProjectIDs []string
	// ProductCounts 每个工程的命中商品数（键是工程 id；只含命中的工程）。
	ProductCounts map[string]int
	// Products 命中商品的采样（每个工程最多 maxRefSamplePerProject 条，稳定排序）。
	Products []RefProduct
	// Total 命中商品总数（精确值，不是采样数）。
	Total int
}

// Referenced 是否命中任何引用（nil 安全）。
func (r *CrossProjectRef) Referenced() bool { return r != nil && r.Total > 0 }

// Outside 只保留「不属于 projectID」的命中（标签删除：本工程照旧解绑、跨工程打回给人）。
//
// 传入空串时返回自身（不做任何过滤）。返回的新对象与自身不共享切片。
func (r *CrossProjectRef) Outside(projectID string) *CrossProjectRef {
	projectID = strings.TrimSpace(projectID)
	if r == nil || projectID == "" {
		return r
	}
	out := &CrossProjectRef{
		RefColumns:    append([]string(nil), r.RefColumns...),
		ProductCounts: make(map[string]int, len(r.ProductCounts)),
	}
	for _, pid := range r.ProjectIDs {
		if pid == projectID {
			continue
		}
		out.ProjectIDs = append(out.ProjectIDs, pid)
		out.ProductCounts[pid] = r.ProductCounts[pid]
		out.Total += r.ProductCounts[pid]
	}
	for _, p := range r.Products {
		if p.ProjectID == projectID {
			continue
		}
		out.Products = append(out.Products, p)
	}
	return out
}

// refScanFilter 在给定句柄上叠加「引用某实体」的谓词（工程谓词与排序由扫描器统一加）。
type refScanFilter func(db *gorm.DB) *gorm.DB

// refRow 扫描的一行（只取定位需要的三列，不加载整行）。
type refRow struct {
	ID        string `gorm:"column:id"`
	ProjectID string `gorm:"column:project_id"`
	SKUCode   string `gorm:"column:sku_code"`
}

// scanProductRefs 逐工程作用域地收集「引用某实体」的商品（跨工程，见文件头）。
//
// projectIDs 是**被扫描的工程清单**（由 service 经 project 契约 List() 取得并传入）：
// model 不读别的模块的表，这里只认一个 id 列表。清单为空时返回空结论（service 在清单
// 拿不到时会先失败，不会用空清单静默放过）。
func (m *Model) scanProductRefs(ctx context.Context, refColumns []string, projectIDs []string, filter refScanFilter) (ref *CrossProjectRef, err error) {
	ref = &CrossProjectRef{
		RefColumns:    refColumns,
		ProductCounts: map[string]int{},
	}
	scopes := normalizeProjectIDs(projectIDs)
	if len(scopes) == 0 {
		return ref, nil
	}
	// 一个事务跑完全部工程：逐工程 SET LOCAL app.project_id（ScopeTx 是事务内设置，
	// 事务结束自动还原 —— 连接池复用不会把上一个工程的作用域泄漏给下一个请求）。
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pid := range scopes {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
			rows := make([]refRow, 0, maxRefSamplePerProject)
			q := filter(tx.WithContext(ctx).Model(&ProductEntity{}).Where("project_id = ?", pid))
			if ferr := q.Select("id, project_id, sku_code").
				Order("create_time ASC, id ASC").
				Limit(maxRefSamplePerProject).
				Find(&rows).Error; ferr != nil {
				return ferr
			}
			if len(rows) == 0 {
				continue
			}
			hits := len(rows)
			if hits >= maxRefSamplePerProject {
				// 采样满额：总数只有 COUNT 知道（采样被 LIMIT 截断过）。
				var cnt int64
				cq := filter(tx.WithContext(ctx).Model(&ProductEntity{}).Where("project_id = ?", pid))
				if cerr := cq.Count(&cnt).Error; cerr != nil {
					return cerr
				}
				hits = int(cnt)
			}
			ref.ProductCounts[pid] = hits
			ref.Total += hits
			ref.Products = append(ref.Products, sampleProducts(rows)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 稳定输出：工程清单升序（scopes 已归一），商品采样按工程 + 商品 id 排序 ——
	// 同一份数据每次得到同一句话，评估与测试都可复现。
	sort.SliceStable(ref.Products, func(i, j int) bool {
		if ref.Products[i].ProjectID != ref.Products[j].ProjectID {
			return ref.Products[i].ProjectID < ref.Products[j].ProjectID
		}
		return ref.Products[i].ProductID < ref.Products[j].ProductID
	})
	for _, pid := range scopes {
		if ref.ProductCounts[pid] > 0 {
			ref.ProjectIDs = append(ref.ProjectIDs, pid)
		}
	}
	return ref, nil
}

// sampleProducts 采样行 → 定位用的商品条目。
func sampleProducts(rows []refRow) []RefProduct {
	out := make([]RefProduct, 0, len(rows))
	for _, r := range rows {
		out = append(out, RefProduct{ProductID: r.ID, ProjectID: r.ProjectID, SKUCode: r.SKUCode})
	}
	return out
}

// normalizeProjectIDs 归一工程清单：去空白、去重、升序（同一批入参只查一次）。
func normalizeProjectIDs(projectIDs []string) []string {
	if len(projectIDs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(projectIDs))
	out := make([]string, 0, len(projectIDs))
	for _, raw := range projectIDs {
		pid := strings.TrimSpace(raw)
		if pid == "" || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
	}
	sort.Strings(out)
	return out
}

// ProductRefsByCategory 跨工程反查「挂了某分类」的商品（删除前引用检查）。
//
// 两个引用面都查：附属分类走 category_ids 的 jsonb 包含谓词（GIN 索引
// idx_products_category_ids），主分类走真列 primary_category_id。
func (m *Model) ProductRefsByCategory(ctx context.Context, categoryID string, projectIDs []string) (*CrossProjectRef, error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductRefs(ctx,
		[]string{"products.category_ids", "products.primary_category_id"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID)
		})
}

// ProductRefsByBrand 跨工程反查「挂了某品牌」的商品（删除前引用检查）。
//
// products.brand_id 有外键 ON DELETE SET NULL：守卫漏看跨工程引用时不会留下悬空 id，
// 而是**静默解绑** —— 别的工程那个商品的品牌区凭空消失，没有任何信号。所以这条同样
// 必须跨工程可发现。
func (m *Model) ProductRefsByBrand(ctx context.Context, brandID string, projectIDs []string) (*CrossProjectRef, error) {
	return m.scanProductRefs(ctx, []string{"products.brand_id"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("brand_id = ?", brandID)
		})
}

// ProductRefsByAttribute 跨工程反查「引用了某属性组」的商品（删除前引用检查）。
//
// attribute_ids 是 JSON 数组、没有数据库级外键，漏看即永久悬空。
func (m *Model) ProductRefsByAttribute(ctx context.Context, attributeID string, projectIDs []string) (*CrossProjectRef, error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductRefs(ctx, []string{"products.attribute_ids"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("attribute_ids @> ?::jsonb", string(probe))
		})
}

// ProductRefsByTag 跨工程反查「挂了某标签」的商品（标签删除前的跨工程检查）。
//
// 标签删除与分类 / 品牌 / 属性不同：**本工程**的引用是主动解绑（补偿式清理，
// 见 RemoveTagFromProductsTx），只有**别的工程**的引用要打回给人 ——
// 跨工程写既会被策略的 WITH CHECK 拒绝（换非超级角色后），也不该由本工程的删除动作
// 替别的工程改数据。判定用 CrossProjectRef.Outside(tag.ProjectID)。
func (m *Model) ProductRefsByTag(ctx context.Context, tagID string, projectIDs []string) (*CrossProjectRef, error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductRefs(ctx, []string{"products.tag_ids"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("tag_ids @> ?::jsonb", string(probe))
		})
}

// ProductRefsByBundleVariant 跨工程反查「把某变体列为捆绑成员」的商品
// （products.bundle_items.options[].variantId，docs/14 §8 的删除守卫补引用面）。
//
// 为什么是 jsonb 查询而不是全表扫 + 内存过滤：成员清单是 JSONB 列，把整个工程的行拉进
// 内存逐个比对在「商品多、每个捆绑都有十几个成员」时是纯粹的浪费，而且会随数据增长变成
// 拖慢删除的隐藏成本。这里用包含语义下推：
//
//	bundle_items @> jsonb_build_object('options', jsonb_build_array(jsonb_build_object('variantId', ?::text)))
//
// 语义与「bundle_items -> 'options' @> […]」等价（PG 的 jsonb 包含是「右边数组的每个元素
// 都被左边某个元素包含」），但**只有整列包含这种写法能用上 GIN 索引** —— 迁移 260 的注释里
// 记了三条 EXPLAIN 实测：GIN 的可索引操作符作用在**被索引的表达式**上，所以「整列索引 +
// 嵌套表达式（bundle_items -> 'options' @> …）」永远走不进索引，写成整列包含才有 Index Cond。
// 索引：idx_products_bundle_items_gin（jsonb_path_ops，占位 260）。
//
// 变体表本身没有 project_id（也不在迁移 215 名单里），但引用面在 products 上 ——
// 这条与其它守卫同形：按工程枚举 products。
func (m *Model) ProductRefsByBundleVariant(ctx context.Context, variantID string, projectIDs []string) (*CrossProjectRef, error) {
	return m.scanProductRefs(ctx, []string{"products.bundle_items.options[].variantId"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("bundle_items @> jsonb_build_object('options', jsonb_build_array(jsonb_build_object('variantId', ?::text)))",
				variantID)
		})
}

// —— 失效扇出用的**全量 id 反查**（审计 ARCH-01 收口票）——
//
// 与上面的 ProductRefsByX 的分工必须说清楚，因为两者长得像但回答的是两个问题：
//   · ProductRefsByX（删除守卫）：回答「全库还有谁引用它」，**采样**（每工程最多
//     maxRefSamplePerProject 个）—— 它要的是一句能给人看的话，不是全集；
//   · ProductIDsByX（失效扇出）：回答「本工程哪些商品的产物会因它改名而变化」，
//     需要**全部 id**。把采样当全集用，就是「只重建了前几个商品」这类静默漏更新
//     —— 站点上表现为一部分商品页永远显示旧分类 / 旧品牌名。
//
// 为什么不跨工程：改名只改本工程的实体行，别的工程里即便有存量跨工程引用，
// 那些商品的构建期解析器按**本工程**作用域取数据（跨工程引用解析不出来），
// 因此跨工程商品页的字节不会因这次改名而变化；同时跨工程写也会被策略的
// WITH CHECK 拒绝。删除守卫必须跨工程（那要拦住删除），失效扇出不必。

// ProductIDsByCategoryTx 本工程内引用该分类的全部商品 id（升序，最多 limit 条）。
//
// 两个引用面都查：附属分类（category_ids 包含谓词，走 GIN 索引）与主分类（真列）。
// tx 必须已由调用方设好工程作用域（products 带 FORCE 策略，缺作用域会静默 0 行）。
func (m *Model) ProductIDsByCategoryTx(ctx context.Context, tx *gorm.DB, projectID, categoryID string, limit int) (ids []string, err error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID)
	})
}

// ProductIDsByBrandTx 本工程内引用该品牌的全部商品 id（升序，最多 limit 条）。
func (m *Model) ProductIDsByBrandTx(ctx context.Context, tx *gorm.DB, projectID, brandID string, limit int) (ids []string, err error) {
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("brand_id = ?", brandID)
	})
}

// ProductIDsByAttributeTx 本工程内引用该属性组的全部商品 id（升序，最多 limit 条）。
func (m *Model) ProductIDsByAttributeTx(ctx context.Context, tx *gorm.DB, projectID, attributeID string, limit int) (ids []string, err error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("attribute_ids @> ?::jsonb", string(probe))
	})
}

// ProductIDsByTagTx 本工程内挂了该标签的全部商品 id（升序，最多 limit 条）。
func (m *Model) ProductIDsByTagTx(ctx context.Context, tx *gorm.DB, projectID, tagID string, limit int) (ids []string, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("tag_ids @> ?::jsonb", string(probe))
	})
}

// scanProductIDsTx 投影 id 的单工程全量查询（四个 ProductIDsByX 共用的唯一实现）。
//
// limit 由调用方给（通常 = 扇出上限 + 1，多取一条用来判断「是否被截断」）：
// 上限留在 service（那是策略），这里只执行。
func (m *Model) scanProductIDsTx(ctx context.Context, tx *gorm.DB, projectID string, limit int,
	filter refScanFilter) (ids []string, err error) {
	if tx == nil || strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}
	q := filter(tx.WithContext(ctx).Model(&ProductEntity{}).Where("project_id = ?", projectID))
	err = q.Select("id::text").Order("id ASC").Limit(limit).Find(&ids).Error
	return ids, err
}

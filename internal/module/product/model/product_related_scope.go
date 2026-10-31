package productmodel

// product_related_scope.go —「这些 id 到底属于谁」的**专用归属诊断**（PROD-01 的拒绝明细）。
//
// 与 ListIDFactsByIDs 的分工（两条问题，不要混）：
//
//	ListIDFactsByIDs(ctx, ids, projectID)  「这批 id 里哪些属于**本工程**」
//	                                       → 一条查询、只认本工程，六个窄读的同一口径。
//	LookupIDOwners(ctx, projectIDs, ids)   「本工程没查到的那些，**别的工程**里有吗」
//	                                       → 逐工程作用域，只回答归属，不返回任何内容。
//
// 为什么需要第二条：resolveRelatedIDs 的拒绝明细要求跨工程条目带上**对方工程 id**
// （PROD-01 验收点：只给商品 id，运营找不到该找谁解除）。而第一类查询按设计读不到
// 外工程的行 —— 拿它去做「跨工程判定」，差集永远落进「不存在」，跨工程那段永远空。
//
// 为什么不直接把 project_id 谓词放宽成「本工程 + 若干个别的工程」：那等于给一个
// **写入口**开了一条任意读别工程的路。本方法只被「本工程已经确认不存在、且不是自引用」
// 的那一小撮 id 调用，且只 Select id / project_id 两列。
//
// 可见性怎么拿到（不依赖连接角色）：工程清单由 service 经 project 契约取（projects 表不带
// project_id、不在迁移 215 名单里，任何角色都读得到），这里逐工程 rls.ScopeTx 设作用域 +
// **显式 project_id 谓词**，与 product_ref_scan.go / product_ref_batch.go 的跨工程扫描同一范式。
// 显式谓词是关键：超级 / BYPASSRLS 角色下策略一行都挡不住，靠的只能是谓词本身。
//
// 边界：
//   · 只读 products，不读变体、不拼别的模块的表；
//   · 清单为空 / ids 为空 → 不查库、返回空结论（调用方保持「不存在」的原有语义）；
//   · 错误原样返回（不吞、不降级成「都算不存在」）—— 归属查不清时宁可整笔拒绝。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductOwner 一个 id 的真实归属（只有身份与工程，没有任何商品内容）。
type ProductOwner struct {
	ID        string
	ProjectID string
}

// LookupIDOwners 在给定的工程清单里查这些 id 的真实归属（逐工程作用域）。
//
// 返回 map[商品 id]所属工程 id。不在任何给定工程里的 id 不出现在结果里 ——
// 调用方按「不存在」处理，与改动前完全一致。
func (m *Model) LookupIDOwners(ctx context.Context, projectIDs, ids []string) (owners map[string]string, err error) {
	owners = map[string]string{}
	scopes := normalizeProjectIDs(projectIDs)
	wanted := normalizeProjectIDs(ids) // 同一套「去空白 + 去空 + 去重」，uuid 列不接受空串
	if len(scopes) == 0 || len(wanted) == 0 {
		return owners, nil
	}
	// 一个事务跑完全部工程：逐工程 SET LOCAL app.project_id（事务结束自动还原，
	// 连接池复用不会把上一个工程的作用域泄漏给下一个请求）。
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pid := range scopes {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
			rows := make([]*ProductEntity, 0, len(wanted))
			if ferr := tx.WithContext(ctx).Model(&ProductEntity{}).
				Select(productIDFactColumns).
				Where("project_id = ? AND id IN ?", pid, wanted).
				Find(&rows).Error; ferr != nil {
				return ferr
			}
			for _, r := range rows {
				if r == nil || strings.TrimSpace(r.ProjectID) == "" {
					continue
				}
				// 先命中先记：一个 id 在数据上只属于一个工程（主键唯一），
				// 这里不覆盖已有结论只是防御。
				if _, ok := owners[r.ID]; !ok {
					owners[r.ID] = r.ProjectID
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return owners, nil
}

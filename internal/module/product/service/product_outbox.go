// product_outbox.go — 商品写路径的静态产物失效 outbox（审计 ARCH-01）。
//
// 要修的口径差：商品主写路径（Create/Update/Delete + 变体 + 定价 + 分类/品牌/标签）
// 此前只调 bumpFragmentCache —— 那只是 Redis 里运行时片段 HTML 的版本号，与静态产物
// 字节毫无关系。于是「改标题 / 改价 / 上下架 / 分类 / 增删商品」之后，详情页与集合
// 列表页的静态产物可以一直是旧字节，且任何日志都不会提示。
//
// 链路（与 content 模块的同名链路逐字对齐）：
//
//	商品写事务内写 outbox 行（project + entity + revision + 依赖键）
//	  → 消费者按批领取（租约 + SKIP LOCKED）
//	  → 窄端口 DependencyInvalidator（装配层注入 pipeline.Fanout）
//	  → page / presentation 的 MarkStaleByDependency → RebuildStale
//
// 三个不变量的落点：
//  1. **事务回滚不产生事件**：行与商品聚合写在同一个事务里（enqueueInvalidationTx 只
//     接受调用方的事务句柄），没有第二处写入、也没有「写完再补偿」；
//  2. **消费者崩溃可重放**：领取只推进 claimed_time / attempts，租约到期后同一批会被
//     重新领取；事件直到被成功消费才写 processed_time；
//  3. **幂等**：失效动作本身幂等（标记 stale 是幂等 UPDATE；重建按当前数据重算），
//     同一批被投递两次不会累积副作用 —— 回归见 public/test/product/feature 的
//     product_outbox_dependency_test.go。
package productservice

import (
	"context"
	"sort"
	"strings"
	"time"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// 消费者参数。
const (
	// outboxBatchMax 单次领取上限：批太大时一次扇出会拖住消费者协程，
	// 批太小则空转；200 与依赖表写入的批大小（CreateInBatches 200）同一量级。
	outboxBatchMax = 200
	// outboxLease 领取租约：超过它未完成即视为消费者已崩溃，可被重新领取。
	// 取 5 分钟：正常消费是「标记 + 触发重建（入队或同步）」的秒级动作，
	// 5 分钟足够覆盖一次慢重建，又不至于让崩溃后的事件等太久。
	outboxLease = 5 * time.Minute
	// outboxDispatchInterval 消费者轮询间隔（装配层默认值）。
	outboxDispatchInterval = 15 * time.Second
)

// SetDependencyInvalidator 注入依赖失效扇出窄端口（装配期调用；可空）。
//
// 为空时：事件照常落库（不丢事实），消费者**不标记处理**，等装配补齐后可重放 ——
// 这正是「静默降级」要防的形态：少了它，商品改了而站点永不更新，且没有报错。
func (s *Service) SetDependencyInvalidator(inv productcontract.DependencyInvalidator) {
	if s == nil {
		return
	}
	s.invalidator = inv
}

// invalidationTarget 一次变更涉及的实体（商品 / 分类 / 品牌 / 标签 / 属性）。
type invalidationTarget struct {
	EntityType string
	EntityID   string
}

// productInvalidationTarget 商品实体的失效目标（最常用的一条）。
func productInvalidationTarget(productID string) invalidationTarget {
	return invalidationTarget{EntityType: productcontract.EntityTypeProduct, EntityID: productID}
}

// membershipDiff 两个成员集合的**对称差**（升序）——「归属变了」的那些实体。
//
// 用在自动标签归属重算上：只重建归属真的变过的商品，而不是整集合都重建一遍。
func membershipDiff(before, after []string) []string {
	inBefore := make(map[string]bool, len(before))
	for _, id := range before {
		if id = strings.TrimSpace(id); id != "" {
			inBefore[id] = true
		}
	}
	inAfter := make(map[string]bool, len(after))
	changed := make([]string, 0, len(after))
	for _, id := range after {
		if id = strings.TrimSpace(id); id != "" {
			inAfter[id] = true
			if !inBefore[id] {
				changed = append(changed, id)
			}
		}
	}
	for _, id := range before {
		if id = strings.TrimSpace(id); id != "" && !inAfter[id] {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed
}

// enqueueInvalidationTx 在**调用方的事务内**写 outbox 行。
//
// 每个目标实体写两类键：
//   - direct_content:{type}:{id} —— 直接引用该实体的产物（商品详情页 / 归档详情）；
//   - content_collection:collection:content:product —— 商品集合（列表页 / 归档列表页）
//     的成员或成员可见字段变化。**任何商品域实体的变更都要发这条键**：列表项里
//     内嵌了商品的名称 / 价格 / 分类品牌标签展示名，改其中任何一个都会改列表字节；
//     而新增 / 删除成员时旧产物里根本没有新实体，只能靠集合键失效
//     （与 content_service.go 的 notifyContentChanged 同一口径）。
//
// projectID 为空（纯单测路径未解析出工程）时跳过：product_outbox_events.project_id
// 是 NOT NULL，编一个工程 id 比跳过更难排查。
func (s *Service) enqueueInvalidationTx(ctx context.Context, tx *gorm.DB, projectID string, targets ...invalidationTarget) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || tx == nil || len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]productmodel.OutboxEventEntity, 0, len(targets)*2)
	seen := map[[2]string]bool{}
	collectionEmitted := false
	for _, t := range targets {
		t.EntityType = strings.TrimSpace(t.EntityType)
		t.EntityID = strings.TrimSpace(t.EntityID)
		key := [2]string{t.EntityType, t.EntityID}
		if t.EntityType == "" || t.EntityID == "" || seen[key] {
			continue
		}
		seen[key] = true
		rev, err := s.m.NextOutboxRevisionTx(ctx, tx, t.EntityType, t.EntityID)
		if err != nil {
			return err
		}
		direct := pipeline.DirectContentKey(t.EntityType, t.EntityID)
		keys := []pipeline.DepKey{direct}
		// 集合键按批去重：它表达的是「商品集合整体变了」，与具体是哪个商品无关，
		// 一批变更发一次即可（消费者按键去重，多发只是多几行无意义的行）。
		if !collectionEmitted {
			collectionEmitted = true
			keys = append(keys, pipeline.ContentCollectionKey(productcontract.EntityTypeProduct))
		}
		for _, k := range keys {
			rows = append(rows, productmodel.OutboxEventEntity{
				ProjectID:      projectID,
				EntityType:     t.EntityType,
				EntityID:       t.EntityID,
				EntityRevision: rev,
				DependencyKind: k.Kind,
				DependencyKey:  k.Key,
				CreateTime:     now,
			})
		}
	}
	return s.m.AppendOutboxTx(ctx, tx, rows)
}

// enqueueProductInvalidationTx 商品变更的快捷入口（单商品）。
func (s *Service) enqueueProductInvalidationTx(ctx context.Context, tx *gorm.DB, projectID, productID string) error {
	return s.enqueueInvalidationTx(ctx, tx, projectID, productInvalidationTarget(productID))
}

// DispatchOutbox 领取并消费一批事件，返回本批处理的条数。
//
// 单批内按 (kind,key) 去重后再交给端口：扇出本身是聚合语义，同一批里同一个键
// 发一次与发 N 次结果相同（重复标记是幂等的 UPDATE）。
func (s *Service) DispatchOutbox(ctx context.Context, limit int) (int, error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	if limit <= 0 || limit > outboxBatchMax {
		limit = outboxBatchMax
	}
	rows, err := s.m.ClaimPendingOutbox(ctx, outboxLease, limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if s.invalidator == nil {
		// 端口未装配：**不标记处理**，事件留在表里等装配补齐后重放。
		return 0, nil
	}
	seen := map[[2]string]bool{}
	done := make([]int64, 0, len(rows))
	for _, row := range rows {
		k := [2]string{row.DependencyKind, row.DependencyKey}
		if !seen[k] {
			seen[k] = true
			// 端口契约：永不返回错误（失败只记日志），因此这里不需要错误分支。
			s.invalidator.Invalidate(ctx, row.DependencyKind, row.DependencyKey)
		}
		done = append(done, row.ID)
	}
	if err := s.m.MarkOutboxProcessed(ctx, done); err != nil {
		// 没标记成功 = 这批会被重新领取（幂等），不打回内容写入。
		return 0, err
	}
	return len(rows), nil
}

// StartOutboxWorker 启动 outbox 消费协程（进程内 goroutine + ticker）。
//
// 端口未注入时直接返回 —— 空转的协程除了刷日志没有任何作用。
// 循环体单独成函数（runOutboxLoop）：装配期入口只做「是否该起协程」的判断，
// 消费节奏与退出的实现细节留在循环里，读的人不必在一个函数里同时看两件事。
func (s *Service) StartOutboxWorker(ctx context.Context, interval time.Duration) {
	if s == nil || s.invalidator == nil {
		return
	}
	if interval <= 0 {
		interval = outboxDispatchInterval
	}
	go s.runOutboxLoop(ctx, interval)
}

// runOutboxLoop 消费循环：首跑先做一次再按 ticker 等间隔（与 webhook 重放调度同一形态）——
// 进程重启后积压的事件立刻被消化，不必等一个轮询间隔。
func (s *Service) runOutboxLoop(ctx context.Context, interval time.Duration) {
	s.dispatchOutboxBatch(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchOutboxBatch(ctx)
		}
	}
}

// dispatchOutboxBatch 消费一批并记录结果（循环里的单次动作）。
func (s *Service) dispatchOutboxBatch(ctx context.Context) {
	n, err := s.DispatchOutbox(ctx, outboxBatchMax)
	if err != nil {
		logger.Scene("product").Error(err, "商品依赖事件消费失败（事件保留待重试）")
		return
	}
	if n > 0 {
		logger.Scene("product").With("count", n).Info("商品依赖事件已扇出")
	}
}

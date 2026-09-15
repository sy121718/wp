// artifact_content_gc.go —— 共享内容对象（content_objects）的标记清除式回收（审计 IDX-016）。
//
// 背景：产物的内容寻址闭包（content_objects + page_artifact_objects）一直只有写入路径，
// 迁移 068 的注释里就记着「content_objects 的 GC 待落地」。产物行被回收
// （payload_state='deleted'）之后，它引用的共享内容对象往往已无人引用，却永远留在表里 ——
// 表只增不减。本文件补上清除侧。
//
// 做法是最朴素的标记清除：标记 =「是否仍被现存产物行引用」，清除无标记的对象。
// 安全默认与产物 GC 保持同一口径（DryRun 默认 true、保留期同一天数），
// 这样两个 GC 在报表里可以放在一起读。
package artifactservice

import (
	"context"
	"fmt"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	"go_wp/pkg/logger"
)

const (
	// defaultContentObjectRetentionDays 内容对象默认保留窗口，与产物 GC 同口径：
	// 窗口内的对象一律不回收 —— 刚发布的内容对象可能只是还没被后续归档复用。
	defaultContentObjectRetentionDays = 30
	// contentObjectGCBatch 单批处理上限：分批的意义是不制造长事务与长锁等待。
	contentObjectGCBatch = 2000
)

// ExternalContentRefs 由其他模块注入：指出入参 hash 中哪些仍被它引用。
//
// 存在的理由是「引用来源不止一处」：page_artifact_objects 是当前唯一的闭包投影，
// 但 content_objects 是共享表，其它模块的产物（DDL 里的 presentation_artifact_objects）
// 将来同样会引用它。孤儿判定必须能问遍所有来源，而不是把「我不知道的引用」当成「没有引用」。
//
// 未注入 = 确认没有任何外部引用来源（当前即此状态：presentation 侧不写 content_objects，
// 见审计 CQ-015）。注入后查询失败一律放弃本轮回收。
type ExternalContentRefs func(ctx context.Context, hashes []string) (referenced []string, err error)

// SetExternalContentRefs 注入外部引用来源；装配期调用一次。
func (s *Service) SetExternalContentRefs(fn ExternalContentRefs) {
	if s == nil {
		return
	}
	s.externalRefs = fn
}

// 单条候选的处理结论（与产物 GC 的 Action 取值同风格）。
const (
	objectActionWouldDelete   = "would_delete"
	objectActionDeleted       = "deleted"
	objectActionKeptExternal  = "kept_external"
	objectActionKeptReclaimed = "kept_reclaimed"
	objectActionDeleteFailed  = "delete_failed"
)

// GarbageCollectContentObjects 回收不再被任何产物行引用的共享内容对象。
//
// 顺序：先数（Orphans = 窗口外的孤儿总数）→ 取一批候选 → 问外部引用 → dryRun 只报告，
// 真删走「语句内复查」的 DELETE（见 model.DeleteOrphanContentObjects）。
//
// 与产物 GC 的两点差异：
//  1. 内容对象本身不在磁盘上（它们是指向产物目录内文件的 Locator 投影），删除只动 DB；
//     产物目录的删除仍由产物 GC 负责，本函数不碰文件系统，因此两个 GC 可以任意顺序，
//     但调用方应「先产物、后内容对象」—— 那样同一轮里刚变成孤儿的对象能立刻被收掉。
//  2. 失败口径是「整轮放弃」而不是「逐条跳过」：问不到外部引用时一个都不删。
//     误删共享对象的代价高于多留一轮垃圾。
func (s *Service) GarbageCollectContentObjects(ctx context.Context, req *artifactdto.ContentObjectGCReq) (res *artifactdto.ContentObjectGCResp, err error) {
	retention, dryRun, limit := defaultContentObjectRetentionDays, true, contentObjectGCBatch
	if req != nil {
		if req.RetentionDays > 0 {
			retention = req.RetentionDays
		}
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -retention)
	res = &artifactdto.ContentObjectGCResp{
		RetentionDays: retention, DryRun: dryRun, Items: []artifactdto.OrphanContentObjectResp{},
	}

	orphans, err := s.model.CountOrphanContentObjects(ctx, before)
	if err != nil {
		return nil, err
	}
	res.Orphans = orphans
	if orphans == 0 {
		return res, nil
	}
	rows, err := s.model.ListOrphanContentObjects(ctx, before, limit)
	if err != nil {
		return nil, err
	}
	res.Scanned = len(rows)
	if len(rows) == 0 {
		return res, nil
	}

	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		hashes = append(hashes, row.ContentHash)
	}
	keptExternal, eerr := s.externalKept(ctx, hashes)
	if eerr != nil {
		return nil, eerr
	}

	if dryRun {
		for _, row := range rows {
			item := orphanItem(row)
			if keptExternal[row.ContentHash] {
				item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
				res.SkippedExtern++
			} else {
				item.Action = objectActionWouldDelete
			}
			res.Items = append(res.Items, item)
		}
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("skippedExternal", res.SkippedExtern).Info("孤儿内容对象预演完成（未删除）")
		return res, nil
	}

	deletable := make([]string, 0, len(rows))
	for _, row := range rows {
		if !keptExternal[row.ContentHash] {
			deletable = append(deletable, row.ContentHash)
		}
	}
	deletedSet, derr := s.deleteOrphans(ctx, deletable, before)
	if derr != nil {
		for _, row := range rows {
			item := orphanItem(row)
			if keptExternal[row.ContentHash] {
				item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
				res.SkippedExtern++
			} else {
				item.Action, item.Reason = objectActionDeleteFailed, derr.Error()
				res.Failed++
			}
			res.Items = append(res.Items, item)
		}
		logger.Scene("artifact").With("failed", res.Failed).Error(derr, "孤儿内容对象回收失败")
		return res, nil
	}

	for _, row := range rows {
		item := orphanItem(row)
		switch {
		case keptExternal[row.ContentHash]:
			item.Action, item.Reason = objectActionKeptExternal, "外部模块仍引用该内容对象"
			res.SkippedExtern++
		case deletedSet[row.ContentHash]:
			item.Action = objectActionDeleted
			res.Deleted++
		default:
			// 候选与删除之间被并发归档重新引用：这是正常赛跑结果（语句内复查拦住了误删），
			// 不计失败也不计跳过 —— 下一轮它若仍是孤儿自会再被选中。
			item.Action, item.Reason = objectActionKeptReclaimed, "删除前已被新产物引用，本轮保留"
		}
		res.Items = append(res.Items, item)
	}
	if res.Deleted > 0 {
		logger.Scene("artifact").With("orphans", res.Orphans).With("scanned", res.Scanned).
			With("deleted", res.Deleted).With("skippedExternal", res.SkippedExtern).
			Info("孤儿内容对象回收完成")
	}
	return res, nil
}

// externalKept 询问外部引用来源，返回「必须保留」的 hash 集合。
// 未注入守卫 = 没有外部来源，返回空集合；注入后失败即向上抛（调用方整轮放弃）。
func (s *Service) externalKept(ctx context.Context, hashes []string) (kept map[string]bool, err error) {
	kept = map[string]bool{}
	if s.externalRefs == nil || len(hashes) == 0 {
		return kept, nil
	}
	referenced, err := s.externalRefs(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", artifactenums.ErrContentRefQueryFailed, err)
	}
	for _, h := range referenced {
		kept[h] = true
	}
	return kept, nil
}

// deleteOrphans 执行删除并把结果转成集合；空入参直接返回空集合（不发语句）。
func (s *Service) deleteOrphans(ctx context.Context, hashes []string, before time.Time) (deleted map[string]bool, err error) {
	deleted = map[string]bool{}
	if len(hashes) == 0 {
		return deleted, nil
	}
	rows, err := s.model.DeleteOrphanContentObjects(ctx, hashes, before)
	if err != nil {
		return nil, err
	}
	for _, h := range rows {
		deleted[h] = true
	}
	return deleted, nil
}

// orphanItem 把一条候选转成响应条目（Action 由调用方按结论填写）。
func orphanItem(row artifactmodel.ContentObjectEntity) artifactdto.OrphanContentObjectResp {
	return artifactdto.OrphanContentObjectResp{
		ContentHash: row.ContentHash, Provider: row.Provider, ObjectKey: row.ObjectKey,
		ByteSize: row.ByteSize, CreatedAt: row.CreatedAt,
	}
}

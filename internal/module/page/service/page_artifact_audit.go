// page_artifact_audit.go —— 产物磁盘与数据库的双向对账（审计 IDX-015）。
//
// 正向（链接是否可达）在 pipeline.AuditActiveLinks；这里补反向：磁盘上的产物目录有没有
// 人认领。两件事分开看是因为处置方式不同 —— 正向异常是「线上已经 404」，必须立刻处理；
// 孤儿只是占磁盘，交给 GC 按保留期回收即可。
package pageservice

import (
	"context"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// SetExternalArtifactOwners 注入「其它模块认领的产物 hash」提供者（装配期调用一次）。
//
// 传 nil 表示没有其它产物来源：此时只按本模块的 page_artifacts 判定归属，
// 别的模块的产物会被报成孤儿 —— 所以装配层应当把 presentation 的清单接进来。
func (s *Service) SetExternalArtifactOwners(provider func(ctx context.Context) ([]string, error)) {
	s.externalArtifactOwners = provider
}

// auditOrphanArtifacts 反向对账：磁盘有、无人认领的产物目录（只报告，不删除）。
func (s *Service) auditOrphanArtifacts(ctx context.Context) (orphans []pagedto.OrphanArtifact, checked int, err error) {
	if s == nil || s.store == nil {
		return nil, 0, nil
	}
	owners := []pipeline.OwnerProvider{
		func(ctx context.Context) ([]string, error) {
			return s.model.ListArtifactHashes(ctx)
		},
	}
	if s.externalArtifactOwners != nil {
		owners = append(owners, s.externalArtifactOwners)
	}
	found, n, aerr := pipeline.AuditOrphanArtifacts(ctx, s.store.Root, owners...)
	if aerr != nil {
		return nil, n, aerr
	}
	orphans = make([]pagedto.OrphanArtifact, 0, len(found))
	var bytes int64
	for _, o := range found {
		orphans = append(orphans, pagedto.OrphanArtifact{Hash: o.Hash, Path: o.Path, Bytes: o.Bytes, Files: o.Files})
		bytes += o.Bytes
	}
	if len(orphans) > 0 {
		// 只记日志不告警：孤儿是可回收的磁盘占用，不是线上故障。
		logger.Scene("page").With("count", len(orphans)).With("bytes", bytes).
			Info("产物对账发现无人认领的目录（交由产物 GC 按保留期回收）")
	}
	return orphans, n, nil
}

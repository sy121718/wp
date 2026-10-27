package presentationmodel

// presentation_publish_record_model.go — 发布记录页要的两条**批量读**（B9）。
//
// 为什么不复用既有的单实例读：ListPublications 一次只吃一个实例 id，而发布记录页要
// 「本页这批实例各自的语言账本」—— 逐实例查就是 N+1，页面上几十行时看起来能忍，
// 上百行时变成一次刷新几十次往返。
//
// 两张表都不带 RLS（迁移 215 的 FORCE 清单只含 presentation_instances），所以这里
// 不需要 InProjectScope；**工程隔离由调用方保证**：它先把已作用域的实例 id 拿到手，
// 再拿这些 id 来查。这条依赖关系写在这里而不是注释里更安全的地方也没有 —— 谁改
// 这两个方法的入参都要看到它。

import "context"

// publishRecordBatchSize 批量 IN (...) 的批次大小。
//
// PG 的绑定参数上限是 65535；取 500 与商品侧 idBatchSize 同口径：一个批次一次往返，
// 500 已覆盖任何一页的实例数。
const publishRecordBatchSize = 500

// ListPublicationsByInstances 批量读多实例的语言账本（按 instance_id 升序、lang 升序）。
func (m *Model) ListPublicationsByInstances(ctx context.Context, instanceIDs []string) (list []PublicationEntity, err error) {
	list = []PublicationEntity{}
	for start := 0; start < len(instanceIDs); start += publishRecordBatchSize {
		end := min(start+publishRecordBatchSize, len(instanceIDs))
		var batch []PublicationEntity
		if err = m.PublicationDB(ctx).
			Where("presentation_id IN ?", instanceIDs[start:end]).
			Order("presentation_id ASC, lang ASC").Find(&batch).Error; err != nil {
			return nil, err
		}
		list = append(list, batch...)
	}
	return list, nil
}

// ArtifactVersionFact 产物的轻量投影：只要版本号与哈希。
//
// 不返回整行：Manifest 是产物里最大的一列，而这一屏只需要「第几版、哪份字节」。
// 与 GetArtifactHash 同一个理由（列白名单写死在 model，调用方拿不到多余的列）。
type ArtifactVersionFact struct {
	ID           string
	Version      int64
	ArtifactHash string
}

// ListArtifactVersionFacts 按产物 id 批量取版本号与哈希。
//
// 找不到的 id 不会出现在结果里（不报错）：账本指向的产物行可能已被 GC 删掉，
// 那是「发布记录还在、产物没了」这种真实状态，由调用方按 0 版本展示。
func (m *Model) ListArtifactVersionFacts(ctx context.Context, artifactIDs []string) (list []ArtifactVersionFact, err error) {
	list = []ArtifactVersionFact{}
	for start := 0; start < len(artifactIDs); start += publishRecordBatchSize {
		end := min(start+publishRecordBatchSize, len(artifactIDs))
		var batch []ArtifactVersionFact
		if err = m.ArtifactDB(ctx).
			Select("id, version, artifact_hash").
			Where("id IN ?", artifactIDs[start:end]).
			Scan(&batch).Error; err != nil {
			return nil, err
		}
		list = append(list, batch...)
	}
	return list, nil
}

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

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// publishRecordBatchSize 批量 IN (...) 的批次大小。
//
// PG 的绑定参数上限是 65535；取 500 与商品侧 idBatchSize 同口径：一个批次一次往返，
// 500 已覆盖任何一页的实例数。
const publishRecordBatchSize = 500

// instancePublishFactColumns 发布登记需要的实例列（投影白名单，写死在本文件）。
//
// instanceRow 只读这八列：身份（id / **project_id** / entity_type / entity_id / instance_role）、
// 路径（url_path）、待重建（stale）与是否已激活（active_artifact_id）。
// override_document 与 render_mode 都可能是大字段，全行取回来只是白搬带宽。
//
// project_id 必须在这张白名单里，而不是「反正 WHERE 里已经限定了工程」：
// 实例实体是**投影的契约**，谁拿到这批行都会拿 ProjectID 判工程（隔离断言、
// 排查时按工程分组）。漏掉这一列不会报错，只会让每一行的 ProjectID 恒为 ""——
// 读起来像「这些实例不属于任何工程」，比少一列更容易被误判成数据坏了。
const instancePublishFactColumns = "id, project_id, entity_type, entity_id, instance_role, url_path, stale, active_artifact_id"

// ListInstanceFactsByEntities 按**实体 id 集合**读实例投影（发布登记用，PRD-011 有状态档）。
//
// 与 ListInstances 的区别有两条，缺一不可：
//
//  1. **过滤下推**：entity_id IN (...) 让数据库筛，而不是把本工程该类型的全部实例搬回来
//     再在 Go 里按 map 过滤。搬回来的行数从「本工程全部实例」降到「调用方点名的实体」。
//  2. **投影收窄**：只取 instancePublishFactColumns 那八列。
//
// 边界要说清楚：这不是「页内有界」——有状态筛选这一档调用方给的是**全工程商品 id**
// （它必须先知道全部发布状态才能过滤），所以本条的行数上界仍是工程内商品数。
// 真正的页内有界需要让上线条件进入正确分页路径，属跨模块口径设计，未在此假装完成。
//
// entityIDs 为 nil / 空串 / 全空白 = **直接返回空集，不查库**：本方法只回答「这批实体
// 有哪些实例」，空集合的答案就是空集。「本工程该类型全部实例」是**另一条**读法，
// 由调用方改用 ListInstances（两边各说各的语义，本方法不退化成全量）。
// 工程作用域仍然必填：presentation_instances 带 FORCE 策略，无作用域读取在非超级
// 角色下静默 0 行。分批 500 与另外两条批量读同口径（PG 绑定参数上限 65535）。
//
// 入参先规范化（trim + 去空 + 保序去重），与 service 的 currentInstances 同一口径：
// entity_id 是 uuid 列，空白串 / 空串原样送进去不是「查不到」，而是整条查询
// invalid input syntax for type uuid（SQLSTATE 22P02）。规范化放在 model 而不是只靠
// 调用方，是因为分批与分批边界都由本方法决定 —— 重复 id 还会让同一行跨批次被搬两遍。
// 规范化后为空（全空白 / 全空串）按空集合处理：同样不查库，不退化成全量。
//
// 顺序按 update_time DESC, id DESC —— 与 ListInstances 逐字相同。跨批次时批次顺序
// 覆盖不了全局顺序，但调用方（发布登记）按 entity_id 建 map，不依赖行顺序。
func (m *Model) ListInstanceFactsByEntities(ctx context.Context, projectID, entityType string, entityIDs []string) (list []*InstanceEntity, err error) {
	ids := normalizeEntityIDs(entityIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	entityType = strings.TrimSpace(entityType)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		for start := 0; start < len(ids); start += publishRecordBatchSize {
			end := min(start+publishRecordBatchSize, len(ids))
			var batch []*InstanceEntity
			q := tx.WithContext(ctx).Model(&InstanceEntity{}).
				Select(instancePublishFactColumns).
				Where("project_id = ?", projectID)
			if entityType != "" {
				q = q.Where("entity_type = ?", entityType)
			}
			if err := q.Where("entity_id IN ?", ids[start:end]).
				Order("update_time DESC, id DESC").Find(&batch).Error; err != nil {
				return err
			}
			list = append(list, batch...)
		}
		return nil
	})
	return list, err
}

// normalizeEntityIDs 去掉空白与空串（保序去重）。
//
// 与 service 的 currentInstances 是同一份口径，两处都留是刻意的：service 那处决定
// 「走不走下推读法」，这里决定「送给 uuid 列的是什么」—— 直接调 model 的调用方
// （测试、将来的新链）不该因为绕过 service 就把 22P02 打出来。
func normalizeEntityIDs(entityIDs []string) []string {
	// 与 service 包的同名函数是不同包里的两份实现，语义必须逐字一致：
	// 改一边就得改另一边，否则「service 认为空集合、model 认为有值」会分叉。
	out := make([]string, 0, len(entityIDs))
	seen := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

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

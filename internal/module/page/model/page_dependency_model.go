package pagemodel

// page_dependency_model.go — page_dependencies 表访问（依赖记录投影，docs/03-pipeline.md §8.2）。
//
// 定位：Artifact Manifest 是历史事实，本表是按 Artifact 可重建的**查询投影**。
// 构建成功后写入，失效查询按 (dependency_kind, dependency_key) 反查受影响的页面。
//
// 反查为什么用「active OR staged」而不是仅 active：
// docs/03-pipeline.md §8.2 只连 pages.active_artifact_id。但未发布的页面
// （只有 staged 产物）在内容变更后不会被标记，其暂存产物会静默过期，
// 一旦发布就上线旧内容。本实现把「当前活跃产物」与「当前暂存产物」两个指针
// 都纳入——两者都只指向一个产物，不含历史噪音，仍是精确集合。

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// tableNamePageDependencies page_dependencies 表名。
const tableNamePageDependencies = "page_dependencies"

// tableNamePageArtifacts 产物表名（依赖行的归属校验要经它 → pages 判断工程）。
const tableNamePageArtifacts = "page_artifacts"

// DependencyEntity page_dependencies 行（产物声明的构建期依赖）。
type DependencyEntity struct {
	PageID         string    `gorm:"column:page_id;primaryKey"`
	ArtifactID     string    `gorm:"column:artifact_id;primaryKey"`
	DependencyKind string    `gorm:"column:dependency_kind;primaryKey"`
	DependencyKey  string    `gorm:"column:dependency_key;primaryKey"`
	Revision       *string   `gorm:"column:revision"`
	LastChecked    time.Time `gorm:"column:last_checked;not null"`
}

// TableName 表名。
func (DependencyEntity) TableName() string { return tableNamePageDependencies }

// DependencyDB 绑定依赖表。
func (m *Model) DependencyDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&DependencyEntity{})
}

// ReplaceDependencies 全量替换某产物的依赖记录（同一事务内 delete + insert）。
//
// 同一产物重复构建（同 hash 原地替换，见 artifact 模块）时依赖集合可能变化，
// 必须整体替换而非累加，否则会残留「旧文档声明过、新产物已不再依赖」的假依赖，
// 造成内容变更时的过度标记。
//
// projectID 必填（DB-009 第四批）：page_dependencies **没有 project_id 列**（不受策略
// 约束），所以「这条依赖记在谁的产物上」只能经 page_artifacts → pages 判断。缺这层校验时，
// 一次越界的 artifactID 就能改写别的工程的依赖投影 —— 而它不报任何错。
func (m *Model) ReplaceDependencies(ctx context.Context, projectID, artifactID string, rows []DependencyEntity) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectRequired
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requireArtifactOwned(ctx, tx, projectID, artifactID); err != nil {
			return err
		}
		if derr := tx.Where("artifact_id = ?", artifactID).Delete(&DependencyEntity{}).Error; derr != nil {
			return derr
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, 200).Error
	})
}

// ListDependencies 读取某产物的全部依赖记录（测试与诊断用，按 kind,key 排序）。
// projectID 必填（DB-009 第四批）：依赖表不受策略约束，归属经 page_artifacts → pages 判断。
func (m *Model) ListDependencies(ctx context.Context, projectID, artifactID string) (list []DependencyEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := m.requireArtifactOwned(ctx, tx, projectID, artifactID); err != nil {
			return err
		}
		return tx.Model(&DependencyEntity{}).Where("artifact_id = ?", artifactID).
			Order("dependency_kind, dependency_key").Find(&list).Error
	})
	return list, err
}

// MarkStaleByDependency 在**指定工程作用域内**按依赖源 (kind,key) 精确标记受影响页面
// 待重建，返回受影响的页面 ID。
//
// 命中条件：该页面的**活跃或暂存**产物在依赖表里声明了这条依赖。
// 语义与旧的全站标记（MarkStaleFor*）严格区分：无关页面不会被触碰，
// 这是 PIPE-3「精确 fan-out」的核心——调用方可用返回的 ID 集合直接断言影响面。
//
// 幂等：已经 stale 的页面重复标记只更新 update_time，返回值仍是完整受影响集合
// （自动重建需要「谁受影响」而不是「谁刚变成 stale」）。
//
// projectID 必填（DB-009 第三批）：页面的可见性由会话变量决定（pages 带 FORCE 策略），
// 而依赖源 key 是跨工程的实体 id（如 article:<uuid>）。调用方（pipeline.Fanout）只带
// (kind,key)，所以「跨工程」这件事由 service 层逐工程各设一次作用域完成；
// 下面的 RETURNING 只回本工程命中的行，两个方向都不会越界。
func (m *Model) MarkStaleByDependency(ctx context.Context, projectID, kind, key string, at time.Time) (ids []string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if kind == "" || key == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		// 影响面摘要不走这条语句：标题 / 路径的取数口径在 service 的 StaleImpactOfIDs
		// （按返回的 ids 反查），让「标 stale」与「读摘要」各自保持单一职责 ——
		// 在这里 JOIN 标题会把两条口径焊死在一条 SQL 里，改任何一个都得动另一个。
		return tx.Raw(`
			WITH affected AS (
				SELECT DISTINCT d.page_id AS page_id
				FROM page_dependencies d
				JOIN pages p ON p.id = d.page_id
				WHERE d.dependency_kind = ?
				  AND d.dependency_key = ?
				  AND p.project_id = ?
				  AND p.deleted_at IS NULL
				  AND d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id)
			)
			UPDATE pages SET stale = true, update_time = ?
			WHERE project_id = ? AND deleted_at IS NULL AND id IN (SELECT page_id FROM affected)
			RETURNING id`, kind, key, projectID, at, projectID).Scan(&ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// requireArtifactOwned 校验产物行属于给定工程（经 page_artifacts → pages）。
func (m *Model) requireArtifactOwned(ctx context.Context, tx *gorm.DB, projectID, artifactID string) error {
	var n int64
	if err := tx.WithContext(ctx).Table(tableNamePageArtifacts).Where("id = ?", artifactID).
		Where("page_id IN (SELECT id FROM "+tableNamePages+" WHERE project_id = ?)", projectID).
		Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CountDependenciesByKind 统计某页面当前活跃产物声明的依赖条数（诊断/测试用）。
// projectID 必填（DB-009 第四批）：同 ReplaceDependencies —— 依赖表不受策略约束。
func (m *Model) CountDependenciesByKind(ctx context.Context, projectID, pageID, kind string) (n int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		return 0, ErrProjectRequired
	}
	err = m.DependencyDB(ctx).
		Where("page_id = ? AND dependency_kind = ?", pageID, kind).
		Count(&n).Error
	return n, err
}

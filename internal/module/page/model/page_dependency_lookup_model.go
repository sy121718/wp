package pagemodel

// page_dependency_lookup_model.go — page_dependencies 的**只读**反查投影。
//
// 与 page_dependency_model.go 的关系：那个文件管写（落依赖行）与「按依赖键把受影响的页面
// 标记 stale」（MarkStaleByDependency，一条 UPDATE）；本文件只回答「谁声明过这条依赖」，
// **全是 SELECT，没有任何写语句** —— 尤其不碰 pages.stale。
//
// 为什么必须与写路径分开而不是复用它：模板引用反查（删除保护 / 影响面提示）只是想看一眼
// 引用关系，调写路径等于顺手把全站页面的 stale 列改了；而且两者回答的集合也不同 ——
// 写路径返回「这次被标记的」，反查要的是「现在声明着这条依赖的」。
//
// 命中口径与 MarkStaleByDependency **逐字一致**（同一张表、同一对产物指针）：
// 该页面的**活跃或暂存**产物声明了这条依赖。刻意不只看 active：未发布的页面
// （只有 staged 产物）同样持有引用，删模板时它会被漏掉（论证见 page_dependency_model.go 的文件头）。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// DependencyRefRow 依赖键反查命中的页面（**最小**只读投影）。
//
// 只有两列：反查的用途是「谁引用了这个键」，路径 / 文档 / 时间都不参与这个判断。
// 也**不**投影 pages.stale：它不是命中口径的一部分（命中只看依赖行 + 产物指针），
// 且当前没有消费端读它 —— 多带一列就等于给契约许一个没人验的承诺。
type DependencyRefRow struct {
	ID    string `gorm:"column:id"`
	Title string `gorm:"column:title"`
}

// ListPagesByDependency 在**指定工程作用域内**按依赖键 (kind,key) 反查声明过该依赖的页面（只读）。
//
// 输出**不排序**：次序口径只留一份（service 侧按 id 归一化排序），这里再排一次就是
// 第二个真相 —— 两处不一致时清单次序会随调用路径变化，而它只影响「同一次渲染里行的先后」，
// 属于最难被发现的那类偏差。
//
// projectID 必填：pages 带 FORCE 策略（谓词读会话变量 app.project_id），不设作用域在换
// 非超级角色后**静默返回 0 行**（fail closed 不报错）—— 删除保护会把「有引用」看成
// 「没有引用」然后放行。page_dependencies 本身没有 project_id 列、不受策略约束，
// 它的归属由 JOIN 到 pages 判定（与 requireArtifactOwned 同一条判据）。
//
// kind / key 任一为空返回空集合：不存在「按空键反查」这回事，用空键去查会把
// 一条缺失的依赖键变成「全站页面都引用了它」。
func (m *Model) ListPagesByDependency(ctx context.Context, projectID, kind, key string) (list []DependencyRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	kind, key = strings.TrimSpace(kind), strings.TrimSpace(key)
	if kind == "" || key == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		// 标题表达式复用 page_stale_model.go 的唯一一份口径（staleTitleExpr）：
		// 反查面上显示的名字必须与待重建清单里那个名字同源，否则同一个页面在两处
		// 显示成两个标题。表达式里的 draft_document 未加表前缀是安全的 ——
		// page_dependencies 没有这一列，PG 只会解析到 pages。
		//
		// pages.deleted_at IS NULL 必须手写：PageEntity.DeletedAt 是 *time.Time（不是
		// gorm.DeletedAt），gorm 的软删谓词不会自动加 —— 漏了它会把已软删的页面
		// 混进删除保护的影响面里。
		return tx.Model(&PageEntity{}).
			Select("pages.id AS id, "+staleTitleExpr+" AS title").
			Joins("JOIN "+tableNamePageDependencies+" d ON d.page_id = pages.id").
			Where("d.dependency_kind = ? AND d.dependency_key = ?", kind, key).
			Where("(d.artifact_id IN (pages.active_artifact_id, pages.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM page_publications WHERE page_id = pages.id UNION SELECT artifact_id FROM page_stagings WHERE page_id = pages.id))").
			Where("pages.project_id = ? AND pages.deleted_at IS NULL", projectID).
			Scan(&list).Error
	})
	return list, err
}

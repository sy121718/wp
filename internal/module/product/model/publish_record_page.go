package productmodel

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/rls"
)

// productPublishRecordPageSize 是发布记录页的默认页容量（调用方未给 size 时的兜底）。
const productPublishRecordPageSize = 20

// 发布记录的三个状态档，与服务层的筛选口径逐字一致。
const (
	publishRecordActive = "active"
	publishRecordDraft  = "draft"
	publishRecordNone   = "none"
)

// publishRecordListColumns 是发布记录页所需的商品投影：列表展示用 name/slug 与排序键，
// 不读图片、描述等大字段。
const publishRecordListColumns = "id, project_id, name, slug, sort, create_time"

// PublishRecordPageRow 是发布记录页的一行商品：商品事实 + 它全部实例、全部语言的发布事实。
type PublishRecordPageRow struct {
	ProductID   string
	ProductName string
	ProductSlug string

	Facts []PublishRecordFactRow
}

// PublishRecordFactRow 是一条「实例 × 语言」发布事实的最小投影（一商品内多实例/多语言各自一行）。
type PublishRecordFactRow struct {
	Lang string

	ActivePath string

	ArtifactVersion int64
	ArtifactHash    string

	PublishedAt time.Time

	Active bool
	Stale  bool
}

// PublishRecordPage 是本页的分页事实：行、筛选后总数、夹过界的页号。
type PublishRecordPage struct {
	Rows  []PublishRecordPageRow
	Total int64

	Page int
}

// 发布记录页的资格语义，逐条对齐发布模块 PublishRecords 端口：
//   - 有实例：本工程、商品实体、任意 role 至少一个（一商品一行，不按 role 分组）；
//   - 已上线：至少一个实例的 active_artifact_id 非空（列是 uuid NULL，NULL 即未上线）。
//
// 两档判定都在候选层完成，于是三档互斥且穷尽：active = 有实例且已上线；
// draft = 有实例但都没上线；none = 一个实例都没有。先筛选、后分页 ——
// total 恒为筛选后的总数，不是先切页再丢行。
const (
	publishRecordInstanceExists = "EXISTS (SELECT 1 FROM presentation_instances pi" +
		" WHERE pi.project_id = products.project_id AND pi.entity_type = ? AND pi.entity_id = products.id)"

	publishRecordInstanceOnline = "EXISTS (SELECT 1 FROM presentation_instances pi" +
		" WHERE pi.project_id = products.project_id AND pi.entity_type = ? AND pi.entity_id = products.id" +
		" AND pi.active_artifact_id IS NOT NULL)"
)

// publishRecordCandidatesCTE 是候选 CTE 的名字：整页语句用 %[1]s 引用它。
const publishRecordCandidatesCTE = "candidates"

// publishRecordCandidatesQuery 是候选来源：工程范围 + 关键词（按字面量匹配）+ 可选状态谓词，
// 全部落在这一层。它只返回一条普通 GORM 查询（表别名 products），
// 由整页语句以 WITH candidates AS (?) 嵌入，占位符因此由同一次 Statement 统一编号。
func (m *Model) publishRecordCandidatesQuery(projectID, keyword, status string) *gorm.DB {
	query := productKeywordFilter(m.db.Model(&ProductEntity{}).
		Select(publishRecordListColumns).Where("project_id = ?", projectID), keyword)
	switch status {
	case publishRecordActive:
		return query.Where(publishRecordInstanceOnline, productcontract.EntityTypeProduct)
	case publishRecordDraft:
		return query.Where(publishRecordInstanceExists+" AND NOT "+publishRecordInstanceOnline,
			productcontract.EntityTypeProduct, productcontract.EntityTypeProduct)
	case publishRecordNone:
		return query.Where("NOT "+publishRecordInstanceExists, productcontract.EntityTypeProduct)
	default:
		return query
	}
}

// publishRecordPageSQL 是整页语句：候选 CTE、产品级 count（capped）、夹页页号（page_no）
// 与本页明细在同一条只读语句里。
//
// 为什么不再「先 count 再 list」：两条语句是两份语句快照，total 与本页行不一定回答同一批商品。
// 一条语句里 count 只看 candidates（产品级，一商品一计数），明细展开发生在 page 之后 ——
// 多实例多语言只在 jsonb 数组里展开，LIMIT 只作用在商品上，total 不随发布实例数放大。
//
// 空集与越界页同一条语句收敛：capped 恒有一行，page 用 LEFT JOIN 挂上去，
// 所以 total=0 也能拿到 total（行是哨兵：product_id 为空，解析时跳过）；
// 空集时候选为空、page 天然 0 行，页号直接取 0，偏移因此是 0
// （OFFSET 不接受负数，SQLSTATE 2201X）；越界页夹到末页页号再乘页大小，避免 page × size 溢出。
//
// 绑定值顺序：候选查询、页偏移、页大小、页大小、页大小、实体类型。
const publishRecordPageSQL = "WITH %[1]s AS (?), " +
	"capped AS (SELECT count(*)::bigint AS total FROM %[1]s), " +
	"page_no AS (SELECT CASE WHEN capped.total = 0 THEN 0 " +
	"ELSE LEAST(?::bigint, (capped.total - 1) / ?::bigint) END AS page_index FROM capped), " +
	"page AS (SELECT c.id, c.name, c.slug, c.sort, c.create_time FROM %[1]s c, page_no p " +
	"ORDER BY c.sort ASC, c.create_time DESC, c.id ASC " +
	"LIMIT ?::bigint OFFSET (SELECT p.page_index * ?::bigint FROM page_no p)) " +
	"SELECT pg.id AS product_id, pg.name AS product_name, pg.slug AS product_slug, " +
	"COALESCE(jsonb_agg(jsonb_build_object(" +
	"'Lang', CASE WHEN d.presentation_id IS NULL THEN '' ELSE COALESCE(d.lang, '') END, " +
	"'ActivePath', COALESCE(d.active_path, ''), " +
	"'ArtifactVersion', COALESCE(d.version, 0), " +
	"'ArtifactHash', CASE WHEN d.presentation_id IS NULL THEN '' " +
	"WHEN d.publication_artifact_id IS NULL THEN '' " +
	"ELSE COALESCE(NULLIF(d.artifact_hash, ''), d.publication_hash, '') END, " +
	// 没有发布行就没有发布时间：保留 SQL NULL，交给 JSON null → Go time.Time 零值。
	// 不能用人造时间兜底 —— timestamptz 字面量会按会话时区（Asia/Shanghai 的历史秒偏移）输出，
	// Go 的 RFC3339 解析会直接失败，而且那是 model 在编造业务时刻。
	"'PublishedAt', d.published_at, " +
	"'Active', d.active, 'Stale', d.stale) " +
	"ORDER BY d.update_time DESC, d.id DESC, d.lang ASC) " +
	"FILTER (WHERE d.id IS NOT NULL), '[]'::jsonb) AS facts, " +
	"capped.total AS total_count, page_no.page_index + 1 AS page " +
	"FROM capped CROSS JOIN page_no LEFT JOIN page pg ON TRUE " +
	"LEFT JOIN LATERAL (" +
	"SELECT i.id, i.update_time, i.stale, " +
	"(i.active_artifact_id IS NOT NULL) AS active, " +
	"pp.presentation_id, pp.lang, pp.active_path, " +
	"pp.artifact_id AS publication_artifact_id, pp.artifact_hash AS publication_hash, " +
	"pp.published_at, a.version, a.artifact_hash " +
	"FROM presentation_instances i " +
	"LEFT JOIN presentation_publications pp ON pp.presentation_id = i.id " +
	"LEFT JOIN presentation_artifacts a ON a.id = pp.artifact_id " +
	"WHERE i.project_id = (SELECT c.project_id::uuid FROM %[1]s c LIMIT 1) " +
	"AND i.entity_type = ? AND i.entity_id = pg.id) d ON TRUE " +
	"GROUP BY pg.id, pg.name, pg.slug, pg.sort, pg.create_time, capped.total, page_no.page_index " +
	"ORDER BY pg.sort ASC, pg.create_time DESC, pg.id ASC"

// publishRecordPageScanRow 是一次语句扫描出来的原始行：明细是 jsonb 聚合，出来是字节。
// 空集时只会有哨兵行（product_id 为空、facts 为空），解析时跳过。
type publishRecordPageScanRow struct {
	ProductID   *string `gorm:"column:product_id"`
	ProductName string  `gorm:"column:product_name"`
	ProductSlug string  `gorm:"column:product_slug"`
	Facts       []byte  `gorm:"column:facts"`
	TotalCount  int64   `gorm:"column:total_count"`
	Page        int     `gorm:"column:page"`
}

// QueryPublishRecordPage 在一次只读语句里交出本页商品、它们的发布明细与精确总数。
//
// 语句与参数由 publishRecordPageStatement 统一编号：候选子查询整体作为一个绑定值嵌入，
// 内层与外层的占位符由同一次 Statement 连续编号，不存在两套混绑。
func (m *Model) QueryPublishRecordPage(ctx context.Context, projectID, keyword, status string,
	size, page int) (PublishRecordPage, error) {
	if size <= 0 {
		size = productPublishRecordPageSize
	}
	if page <= 0 {
		page = 1
	}
	raw := make([]publishRecordPageScanRow, 0, size)
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.publishRecordPageStatement(tx, projectID, keyword, status, size, page).Scan(&raw).Error
	})
	if err != nil {
		return PublishRecordPage{}, err
	}
	return decodePublishRecordPageRow(raw)
}

// publishRecordPageStatement 组装整页语句：候选查询先绑到事务连接上（保留工程、关键词与状态谓词），
// 再作为第一个绑定值交给外层 Raw，其余绑定值是页偏移与页大小。
func (m *Model) publishRecordPageStatement(tx *gorm.DB, projectID, keyword, status string,
	size, page int) *gorm.DB {
	candidates := m.publishRecordCandidatesQuery(projectID, keyword, status)
	q := fmt.Sprintf(publishRecordPageSQL, publishRecordCandidatesCTE)
	return tx.Raw(q, candidates, page-1, size, size, size, productcontract.EntityTypeProduct)
}

// decodePublishRecordPageRow 把扫描行还原成业务事实：明细顺序（实例新→旧、语言升序）由 SQL 固定，
// 哨兵行（product_id 为空）只表示「本页 0 行」，直接跳过。
func decodePublishRecordPageRow(raw []publishRecordPageScanRow) (PublishRecordPage, error) {
	out := PublishRecordPage{Rows: make([]PublishRecordPageRow, 0, len(raw)), Page: 1}
	for _, r := range raw {
		if r.TotalCount > out.Total {
			out.Total = r.TotalCount
		}
		if r.Page > 0 {
			out.Page = r.Page
		}
		if r.ProductID == nil || *r.ProductID == "" {
			continue
		}
		row := PublishRecordPageRow{
			ProductID:   *r.ProductID,
			ProductName: r.ProductName,
			ProductSlug: r.ProductSlug,
			Facts:       []PublishRecordFactRow{},
		}
		if len(r.Facts) > 0 {
			if err := json.Unmarshal(r.Facts, &row.Facts); err != nil {
				return PublishRecordPage{}, err
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

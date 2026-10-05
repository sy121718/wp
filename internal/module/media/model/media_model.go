// Package mediamodel 媒体模块的数据库模型层，封装 sys_attachment 和 sys_file_category 表的 CRUD 操作。
package mediamodel

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

const (
	tableNameSysAttachment   = "sys_attachment"
	tableNameSysFileCategory = "sys_file_category"
)

const (
	AttachmentStatusDisabled = 0
	AttachmentStatusEnabled  = 1
)

// AttachmentEntity 对应 sys_attachment 表。
type AttachmentEntity struct {
	ID          uint64  `gorm:"column:id;primaryKey"`
	CategoryID  *uint64 `gorm:"column:category_id"`
	FileName    string  `gorm:"column:file_name"`
	FilePath    string  `gorm:"column:file_path"`
	FileSize    int64   `gorm:"column:file_size"`
	FileType    string  `gorm:"column:file_type"`
	MimeType    *string `gorm:"column:mime_type"`
	StorageType string  `gorm:"column:storage_type;default:local"`
	StoragePath *string `gorm:"column:storage_path"`
	URL         *string `gorm:"column:url"`
	MD5         *string `gorm:"column:md5"`
	ExtraInfo   *string `gorm:"column:extra_info"`
	// Generation 换图代数（迁移 067）：初始 1，每次换图 +1，供依赖记录/构建期重建判定。
	Generation int        `gorm:"column:generation;not null;default:1"`
	Status     int        `gorm:"column:status;default:1"`
	CreateBy   *uint64    `gorm:"column:create_by"`
	UpdateBy   *uint64    `gorm:"column:update_by"`
	CreateTime time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

func (AttachmentEntity) TableName() string { return tableNameSysAttachment }

// FileCategoryEntity 对应 sys_file_category 表。
type FileCategoryEntity struct {
	ID           uint64     `gorm:"column:id;primaryKey"`
	CategoryName string     `gorm:"column:category_name"`
	CategoryCode string     `gorm:"column:category_code;uniqueIndex"`
	ParentID     uint64     `gorm:"column:parent_id;default:0"`
	SortOrder    int        `gorm:"column:sort_order;default:0"`
	Icon         *string    `gorm:"column:icon"`
	Status       int        `gorm:"column:status;default:1"`
	CreateBy     *uint64    `gorm:"column:create_by"`
	UpdateBy     *uint64    `gorm:"column:update_by"`
	CreateTime   *time.Time `gorm:"column:create_time"`
	UpdateTime   *time.Time `gorm:"column:update_time"`
}

func (FileCategoryEntity) TableName() string { return tableNameSysFileCategory }

// AttachmentModel 封装 sys_attachment 表的数据访问。
type AttachmentModel struct {
	db *gorm.DB
}

// FileCategoryModel 封装 sys_file_category 表的数据访问。
type FileCategoryModel struct {
	db *gorm.DB
}

// NewAttachmentModel 创建附件模型。
func NewAttachmentModel(db *gorm.DB) *AttachmentModel {
	return &AttachmentModel{db: db}
}

// NewFileCategoryModel 创建分类模型。
func NewFileCategoryModel(db *gorm.DB) *FileCategoryModel {
	return &FileCategoryModel{db: db}
}

// attrDB 返回绑定 AttachmentEntity 的 GORM DB 实例。
func (m *AttachmentModel) attrDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AttachmentEntity{})
}

// catDB 返回绑定 FileCategoryEntity 的 GORM DB 实例。
func (m *FileCategoryModel) catDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&FileCategoryEntity{})
}

// --- AttachmentModel 方法 ---

// Transaction 起事务并把 *gorm.DB 句柄交给调用方编排（AGENTS.md「写操作的事务与回滚」）。
//
// 定位：model 不自持事务边界。media 的写路径横跨「附件行 + 变体行」两张表，
// 单一真源要求这两处同事务，边界由 service 决定（见 media_crud.go 的 Upload 注释）。
// 事务内一律走本文件的 …Tx 方法，不混用自带连接的非 Tx 形态。
func (m *AttachmentModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// Create 新增一条附件记录。
func (m *AttachmentModel) Create(ctx context.Context, e *AttachmentEntity) error {
	return m.attrDB(ctx).Create(e).Error
}

// GetByID 根据 ID 查询附件（仅启用记录，软删除后不可见）。
func (m *AttachmentModel) GetByID(ctx context.Context, id uint64) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).Where("id = ? AND status = ?", id, AttachmentStatusEnabled).First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetByFilePath 按存储相对路径查询启用状态的附件（构建期资源探测用）。
// 兼容历史数据中带/不带前导斜杠两种写法。
func (m *AttachmentModel) GetByFilePath(ctx context.Context, filePath string) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).
		Where("status = ?", AttachmentStatusEnabled).
		Where("file_path = ? OR file_path = ?", filePath, "/"+filePath).
		First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetByMD5AndType 按「md5 + 文件类型」查询启用中的附件（上传去重键）。
// 同一内容多条记录时取最早一条（id ASC），保证去重命中结果稳定。
func (m *AttachmentModel) GetByMD5AndType(ctx context.Context, md5 string, fileType string) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).
		Where("md5 = ? AND file_type = ? AND status = ?", md5, fileType, AttachmentStatusEnabled).
		Order("id ASC").
		First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ReplaceContentMeta 换图后的元数据回填：generation 自增与 md5/大小/MIME 一次写完。
//
// 为什么不能分两条 SQL：IncrementGeneration 与 AttachmentUpdate 之间若失败/进程退出，
// 磁盘已是新内容而 DB 仍标旧 md5 与旧代数 —— 之后同内容上传会命中「内容未变」幂等分支
// 返回错误现状，去重键与依赖重建判定双双失真，且没有自愈路径。
// 单条 UPDATE 把这个窗口压到一次语句提交，并用 RETURNING 取回新代数。
func (m *AttachmentModel) ReplaceContentMeta(ctx context.Context, id uint64, md5hex string, size int64, mimeType string, updatedAt time.Time) (int, error) {
	var gen int
	err := m.db.WithContext(ctx).Raw(
		"UPDATE "+tableNameSysAttachment+
			" SET generation = generation + 1, md5 = ?, file_size = ?, mime_type = ?, update_time = ?"+
			" WHERE id = ? RETURNING generation",
		md5hex, size, mimeType, updatedAt, id).Scan(&gen).Error
	if err != nil {
		return 0, err
	}
	return gen, nil
}

// HardDelete 物理删除附件记录（上传两阶段登记失败时的回滚动作；
// 常规删除走 Delete 软删，见 service 的引用保护）。
func (m *AttachmentModel) HardDelete(ctx context.Context, id uint64) error {
	return m.attrDB(ctx).Where("id = ?", id).Delete(&AttachmentEntity{}).Error
}

// List 分页查询附件，支持按文件类型和分类过滤。
func (m *AttachmentModel) List(ctx context.Context, fileType string, categoryID *uint64, uncategorized bool, search string, offset, limit int) ([]AttachmentEntity, int64, error) {
	q := m.attrDB(ctx).Where("status = ?", AttachmentStatusEnabled)
	if fileType != "" {
		q = q.Where("file_type = ?", fileType)
	}
	if categoryID != nil && *categoryID > 0 {
		q = q.Where("category_id = ?", *categoryID)
	}
	if uncategorized {
		q = q.Where("category_id IS NULL")
	}
	if search != "" {
		// LIKE 通配符转义：_ / % 按字面匹配（ESCAPE '\'）。
		//
		// 用 ILIKE 而不是 LIKE：商品与内容两处都已经是 ILIKE，只有这里区分大小写 ——
		// 用户说「logo」时查不到 Logo.png，而工具说明写的是「按文件名搜索」，
		// 他会以为这个文件不存在。文件名的大小写不是用户要记的东西。
		q = q.Where("file_name ILIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(search)+"%")
	}

	// uncategorized：只取**没有分类**的那些（category_id IS NULL）。
	//
	// 不能靠「传 categoryID=0」表达这件事：上面那条分支的判据是 `*categoryID > 0`，
	// 所以 0 等于**不过滤**（返回全部），与调用方想说的「未分类」正好相反 ——
	// 而 media_update 里 0 又表示「移入未分类」，同一个 0 在两个工具里语义相反，
	// 模型按字面理解必然踩中其中一个。
	if uncategorized {
		q = q.Where("category_id IS NULL")
	}

	return m.listPage(q, offset, limit, nil, 0)
}

// ListAfter 按创建时间和 ID 的复合键取下一页，避免深分页扫描并丢弃大量行。
func (m *AttachmentModel) ListAfter(ctx context.Context, fileType string, categoryID *uint64, uncategorized bool, search string, after time.Time, afterID uint64, limit int) ([]AttachmentEntity, int64, error) {
	q := m.attrDB(ctx).Where("status = ?", AttachmentStatusEnabled)
	if fileType != "" {
		q = q.Where("file_type = ?", fileType)
	}
	if categoryID != nil && *categoryID > 0 {
		q = q.Where("category_id = ?", *categoryID)
	}
	if uncategorized {
		q = q.Where("category_id IS NULL")
	}
	if search != "" {
		q = q.Where("file_name ILIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(search)+"%")
	}
	return m.listPage(q, 0, limit, &after, afterID)
}

func (m *AttachmentModel) listPage(q *gorm.DB, offset, limit int, after *time.Time, afterID uint64) ([]AttachmentEntity, int64, error) {
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if after != nil {
		q = q.Where("(create_time, id) < (?, ?)", *after, afterID)
	}
	var list []AttachmentEntity
	if err := q.Order("create_time DESC, id DESC").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// Delete 软删除附件（设置 status=0）。
func (m *AttachmentModel) Delete(ctx context.Context, id uint64) error {
	return m.attrDB(ctx).Where("id = ?", id).Update("status", AttachmentStatusDisabled).Error
}

// --- FileCategoryModel 方法 ---

// ListAll 查询所有启用的分类，按 sort_order, id 排序。
func (m *FileCategoryModel) ListAll(ctx context.Context) ([]FileCategoryEntity, error) {
	var list []FileCategoryEntity
	err := m.catDB(ctx).Where("status = ?", 1).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// --- 分类 CRUD（媒体库左树管理） ---

// CreateCategory 新建分类（ParentID=0 为顶级）。
func (m *FileCategoryModel) CreateCategory(ctx context.Context, e *FileCategoryEntity) error {
	return m.catDB(ctx).Create(e).Error
}

// GetCategory 按 ID 查询分类（仅启用记录，软删除分类不可作父级/目标）。
func (m *FileCategoryModel) GetCategory(ctx context.Context, id uint64) (*FileCategoryEntity, error) {
	e := &FileCategoryEntity{}
	if err := m.catDB(ctx).Where("id = ? AND status = ?", id, 1).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateCategory 更新分类（仅非 nil 字段）。
func (m *FileCategoryModel) UpdateCategory(ctx context.Context, id uint64, updates map[string]any) error {
	return m.catDB(ctx).Where("id = ?", id).Updates(updates).Error
}

// HasChildren 判断分类是否存在启用子级。
func (m *FileCategoryModel) HasChildren(ctx context.Context, id uint64) (bool, error) {
	var n int64
	err := m.catDB(ctx).Where("parent_id = ? AND status = ?", id, 1).Count(&n).Error
	return n > 0, err
}

// DeleteCategory 软删除分类（status=0）。
func (m *FileCategoryModel) DeleteCategory(ctx context.Context, id uint64) error {
	return m.catDB(ctx).Where("id = ?", id).Update("status", 0).Error
}

// AttachmentUpdate 更新附件字段（文件名 / 分类 / ExtraInfo JSON）。
func (m *AttachmentModel) AttachmentUpdate(ctx context.Context, id uint64, updates map[string]any) error {
	return m.attrDB(ctx).Where("id = ?", id).Updates(updates).Error
}

// AttachmentUpdateTx 在调用方给的事务句柄上回填附件字段。
//
// 上传的「落盘后回填」必须与同一批的变体登记同事务（Upload 的两阶段登记第二段）：
// 只在事务内写变体、事务外写元数据，一旦进程在这两步之间退出，就会留下
// 「文件已落盘、附件行仍是草稿（status=0）、变体行却已有」的错位状态 ——
// 访问面看不到这个附件，而变体记录又占着位。事务边界由 service 决定。
func (m *AttachmentModel) AttachmentUpdateTx(ctx context.Context, tx *gorm.DB, id uint64, updates map[string]any) error {
	return tx.WithContext(ctx).Model(&AttachmentEntity{}).Where("id = ?", id).Updates(updates).Error
}

// ListForAudit 只读列出附件行（含 status=0 的历史行），按 id 升序，供存储对账扫描。
//
// 为什么不能改用分页 List：对账要的是「全量真相」——分页 List 只取 status=1，
// 而「草稿残留」（status=0 且 file_path 为空）恰恰是待发现的对象之一。
// 仍带 limit：对账是一次人工触发的巡检，不是请求路径上的查询，宁可截断并显式
// 报告「清单被截断」，也不做无上限的全表拉取。
func (m *AttachmentModel) ListForAudit(ctx context.Context, limit int) ([]AttachmentEntity, error) {
	var list []AttachmentEntity
	err := m.attrDB(ctx).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// CountForAudit 统计附件行总数（对账报告里判断「清单是否被截断」用）。
func (m *AttachmentModel) CountForAudit(ctx context.Context) (int64, error) {
	var n int64
	err := m.attrDB(ctx).Count(&n).Error
	return n, err
}

// extraKeyRe ExtraInfo 业务键名白名单格式：内联进 SQL 之前逐键校验，杜绝拼接注入。
var extraKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// MergeAttachmentExtra 原子合并 ExtraInfo 的业务键（alt/title/description）。
//
// 为什么不能用「整列读-改-写」：extra_info 同一列还存着构建期写入的 refs
// 引用缓存（见 media_ref_model.go 的 AddRef/ReplaceRefs）。两者并发时，
// 整列覆盖会把刚写进去的 refs 回退成旧值 → 引用保护失效（在用中的媒体被误删）。
// 这里改成 SQL 级 jsonb 合并（与 AddRef 同思路），两条写入路径互不覆盖。
//
// 语义：
//   - extra_info 为 SQL NULL / jsonb null 时按空对象处理（正常写入，与旧实现一致）；
//   - 数组 / 字符串 / 数字等历史脏数据原样保留，不归零重写；
//   - set 中的键覆盖写入，del 中的键删除；两者都在一次 UPDATE 内完成。
func (m *AttachmentModel) MergeAttachmentExtra(ctx context.Context, id uint64, set map[string]any, del []string) error {
	setJSON := "{}"
	if len(set) > 0 {
		raw, err := json.Marshal(set)
		if err != nil {
			return err
		}
		setJSON = string(raw)
	}
	// 基值：NULL / jsonb null → 空对象；object → 自身。
	// jsonb_typeof(NULL) 为 NULL，`IN (...)` 判定不成立，因此 NULL 会落到 ELSE 分支，
	// 经 COALESCE 归零为空对象 —— 这正是旧实现（从空 map 起手）的行为。
	base := "COALESCE(NULLIF(extra_info, 'null'::jsonb), '{}'::jsonb)"
	// 括号不能省：PG 对同级运算符不按左结合解析（实测 'a'||'b'-'c' 会算成 'a'||('b'-'c')），
	// 必须显式写成 ((base || 补丁) - 删除键) 才能正确删键。
	expr := "CASE WHEN jsonb_typeof(extra_info) IN ('array','string','number','boolean') THEN extra_info ELSE ((" + base + " || ?::jsonb)"
	for _, k := range del {
		// 键名内联（先过白名单校验）而不是用占位符：GORM 对 Update 表达式参数与
		// Where 参数的拼接顺序不保证与书写顺序一致，多占位符会导致实参错位；
		// 键名来自服务端枚举的常量，校验格式后内联无注入面。
		if !extraKeyRe.MatchString(k) {
			return fmt.Errorf("非法的 ExtraInfo 键名: %q", k)
		}
		expr += " - '" + k + "'::text"
	}
	expr += ") END"
	return m.attrDB(ctx).Where("id = ?", id).
		Update("extra_info", gorm.Expr(expr, setJSON)).Error
}

// DetachAttachments 把分类下的全部附件移入未分类（category_id=NULL，分类删除前的级联动作）。
func (m *AttachmentModel) DetachAttachments(ctx context.Context, categoryID uint64) error {
	return m.attrDB(ctx).Where("category_id = ?", categoryID).Update("category_id", nil).Error
}

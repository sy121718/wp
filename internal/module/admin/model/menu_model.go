// Package adminmodel 合并后的统一模型包。
package adminmodel

import (
	"context"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

const tableNameSysMenus = "sys_menus"

// tableNameSysMenuPermission 是菜单 ↔ 权限点多对多关联表（迁移 470）。
//
// 它属于菜单聚合的一部分，读写在 MenuModel 内编排 —— 不另开一个 Model：
// 「菜单行 + 它的权限码集合」必须原子写入（见 CreateWithPermissionCodes），
// 跨 Model 编排就得把 *gorm.DB 递出事务边界，而 service 调 DB(ctx) 是禁止的。
const tableNameSysMenuPermission = "sys_menu_permission"

const (
	MenuTypeDirectory = 1 // 目录
	MenuTypeMenu      = 2 // 菜单
	MenuTypeButton    = 3 // 按钮
	MenuTypeIframe    = 4 // iframe
	MenuTypeExternal  = 5 // 外链
)

const (
	MenuStatusDisabled = 0
	MenuStatusEnabled  = 1
)

// MenuEntity 对应 sys_menus 表。
type MenuEntity struct {
	ID uint64 `gorm:"column:id;primaryKey"`
	// PermissionCode 是迁移 470 之前的单值列，**已降级为兼容写入点**：
	// 仍会被写入（seed / 历史迁移 / 本模块保存时写集合首码），因为 24 条 seed 直写这一列，
	// 其中 4 条（090/101/126/128）拿它当幂等判据 —— 删列或停写会让它们每次启动重跑、重复插菜单。
	// **读路径一律只用 PermissionCodes**，不要再用这个字段做授权判断。
	PermissionCode *string `gorm:"column:permission_code"`
	// PermissionCodes 是「这个菜单节点代表哪些权限」的唯一真源（sys_menu_permission）。
	// 非列：由各查询方法返回前装配（attachPermissionCodes）；
	// 写入见 CreateWithPermissionCodes / UpdateWithPermissionCodes（同一事务内写两张表）。
	PermissionCodes []string `gorm:"-"`
	Title           string   `gorm:"column:title"`
	TitleKey        *string  `gorm:"column:title_key"`
	ParentID        uint64   `gorm:"column:parent_id;default:0"`
	Type            int      `gorm:"column:type;default:2"`
	Path            string   `gorm:"column:path"`
	Component       string   `gorm:"column:component"`
	ExternalURL     string   `gorm:"column:external_url"`
	Icon            string   `gorm:"column:icon"`
	// Status 不带 gorm default tag：避免 gorm 把显式 0（禁用）改写为 1（见 SysRuleEntity.Status 注释）。
	Status     int        `gorm:"column:status"`
	IsHidden   int        `gorm:"column:is_hidden;default:0"`
	IsPublic   int        `gorm:"column:is_public;default:0"`
	IsSystem   int        `gorm:"column:is_system;default:0"`
	SortOrder  int        `gorm:"column:sort_order;default:0"`
	Remark     *string    `gorm:"column:remark"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time"`
	UpdateBy   uint64     `gorm:"column:update_by"`
	UpdateTime *time.Time `gorm:"column:update_time"`
	DeletedAt  *time.Time `gorm:"column:deleted_at"`
}

// TableName 返回 sys_menus 表名。
func (MenuEntity) TableName() string {
	return tableNameSysMenus
}

// MenuModel 菜单数据访问。
type MenuModel struct {
	db *gorm.DB
}

// NewMenuModel 创建菜单数据访问实例。
func NewMenuModel(db *gorm.DB) *MenuModel {
	return &MenuModel{db: db}
}

// DB 返回绑定当前菜单表的 GORM 查询上下文。
func (m *MenuModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&MenuEntity{})
}

// BeforeCreate 创建前补齐时间。
func (e *MenuEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

// BeforeUpdate 更新前刷新时间。
func (e *MenuEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

// GetByID 根据 ID 查询菜单，不存在返回 nil。
func (m *MenuModel) GetByID(ctx context.Context, id uint64) (*MenuEntity, error) {
	var entity MenuEntity
	err := m.DB(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	rows := []MenuEntity{entity}
	if err := m.fillPermissionCodes(ctx, rows); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// ListAll 查询全部未删除菜单，按 sort_order、id 排序。
//
// 含禁用项与按钮（type=3）：菜单管理页要能看到并编辑它们。
// 只做导航 / 授权构建的路径用 ListEnabled —— 那个条件与用户无关，下推到 SQL 更省。
func (m *MenuModel) ListAll(ctx context.Context) ([]MenuEntity, error) {
	var list []MenuEntity
	err := m.DB(ctx).Where("deleted_at IS NULL").Order("sort_order ASC, id ASC").Find(&list).Error
	if err != nil {
		return nil, err
	}
	if err := m.fillPermissionCodes(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// MenuParentOption is the small, unpaged projection needed by the parent selector.
type MenuParentOption struct {
	ID        uint64
	ParentID  uint64
	Title     string
	Type      int
	SortOrder int
}

// ListPage counts and fetches matching live menus without materializing the entire tree.
func (m *MenuModel) ListPage(ctx context.Context, page, limit int, keyword string) (int64, []MenuEntity, error) {
	query := m.DB(ctx).Where("deleted_at IS NULL")
	if keyword != "" {
		pattern := "%" + database.EscapeLikePattern(keyword) + "%"
		query = query.Where("title LIKE ? ESCAPE '\\' OR path LIKE ? ESCAPE '\\' OR remark LIKE ? ESCAPE '\\'", pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, nil, err
	}
	var rows []MenuEntity
	if total > 0 && int64(page-1) > (total-1)/int64(limit) {
		page = int((total-1)/int64(limit)) + 1
	}
	err := query.Order("sort_order ASC, id ASC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	if err != nil {
		return 0, nil, err
	}
	if err := m.fillPermissionCodes(ctx, rows); err != nil {
		return 0, nil, err
	}
	return total, rows, nil
}

// ListParentOptions reads only the columns required to keep the selector complete.
func (m *MenuModel) ListParentOptions(ctx context.Context) ([]MenuParentOption, error) {
	var options []MenuParentOption
	err := m.DB(ctx).Select("id, parent_id, title, type, sort_order").
		Where("deleted_at IS NULL").Order("sort_order ASC, id ASC").Find(&options).Error
	return options, err
}

// ListEnabled 查询启用且未删除的菜单，按 sort_order、id 排序。
//
// 导航树与动态路由都只认 status=1（禁用项从不进树），这是与用户无关的静态条件，
// 放在 SQL 里可以少读一半行 —— 更何况 type=3 的按钮权限点通常占表里的大头。
func (m *MenuModel) ListEnabled(ctx context.Context) ([]MenuEntity, error) {
	var list []MenuEntity
	err := m.DB(ctx).
		Where("deleted_at IS NULL AND status = ?", MenuStatusEnabled).
		Order("sort_order ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	if err := m.fillPermissionCodes(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// ListByIDs 按 ID 列表批量查询。
func (m *MenuModel) ListByIDs(ctx context.Context, ids []uint64) ([]MenuEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []MenuEntity
	err := m.DB(ctx).Where("id IN ? AND deleted_at IS NULL", ids).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// menuUpdateColumns 是 UpdateWithPermissionCodes 显式持久化的列清单。
//
// 必须显式列出（而不是让 gorm 自行推断）：gorm 默认跳过零值字段，于是「把备注清空」
// 「把图标去掉」这类编辑会被静默丢弃 —— 表单里删掉的内容下次打开又回来了。
// **permission_code 刻意不在这个清单里**：旧列自迁移 470 起只作 seed / 历史迁移的写入通道，
// 界面不再碰它。理由不是「省一列」，而是 12 条 seed 语句拿它当幂等判据
// （030/086b/090/093/097/101/107/110 的插入判据，126/128/132/133 的联合判据），
// 而其中「货源管理」「采购入库」各 3 条按钮是**同父菜单、同标题、只有码不同** ——
// 换成 title 判据会互相挡住（少插两条），换成 code 判据又被界面改码打乱。
// 让界面完全不写这一列，判据就永远稳定：新库重放时 seed 写旧列、触发器补进新表；
// 界面改码只动新表。读路径本来也只读新表（见 MenuEntity.PermissionCodes）。
//
// 顺带一个必须一起守的约束：不 SET 这一列 → 不触发 trg_sys_menus_sync_menu_permission，
// 于是「界面上删掉的码」不会被触发器按旧列的值补回来。
var menuUpdateColumns = []string{
	"title",
	"title_key",
	"parent_id",
	"type",
	"path",
	"component",
	"external_url",
	"icon",
	"status",
	"is_hidden",
	"is_public",
	"sort_order",
	"remark",
	"update_time",
}

// CountByParentID 统计子菜单数量。
func (m *MenuModel) CountByParentID(ctx context.Context, parentID uint64) (int64, error) {
	var count int64
	err := m.DB(ctx).Where("parent_id = ? AND deleted_at IS NULL", parentID).Count(&count).Error
	return count, err
}

// --- 菜单 ↔ 权限码（sys_menu_permission，迁移 470）---

// ListPermissionCodesByMenuIDs 批量取 menu_id → 权限码集合。
//
// 一次 IN 查询服务整批菜单：菜单列表页、权限树、导航树与它的 30s 缓存都走它，
// 逐条查会退化成 N+1（列表页一次就是 20 条，导航树一次上百条）。
func (m *MenuModel) ListPermissionCodesByMenuIDs(ctx context.Context, menuIDs []uint64) (map[uint64][]string, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	type row struct {
		MenuID         uint64
		PermissionCode string
	}
	var rows []row
	err := m.db.WithContext(ctx).Table(tableNameSysMenuPermission).
		Select("menu_id", "permission_code").
		Where("menu_id IN ?", menuIDs).
		Order("menu_id ASC, permission_code ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint64][]string, len(rows))
	for _, r := range rows {
		out[r.MenuID] = append(out[r.MenuID], r.PermissionCode)
	}
	return out, nil
}

// fillPermissionCodes 就地给一批菜单填充权限码集合。
//
// 参数是值切片而不是实体：切片的元素可寻址，list[i] 的修改对调用方可见，
// 于是各查询方法可以「Find 之后补一次」，不需要把签名改成指针切片。
func (m *MenuModel) fillPermissionCodes(ctx context.Context, list []MenuEntity) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	byMenu, err := m.ListPermissionCodesByMenuIDs(ctx, ids)
	if err != nil {
		return err
	}
	for i := range list {
		if codes, ok := byMenu[list[i].ID]; ok {
			list[i].PermissionCodes = codes
		}
	}
	return nil
}

// ListMenuIDsByPermissionCodes 按权限码反查菜单 id（去重、升序）。
//
// 只查关联表、不 join sys_menus：model 层不做多表关联（internal/module/CLAUDE.md）。
// 关联行不会指向不存在的菜单（只有本模块写这张表），但可能是**已软删菜单**留下的 ——
// 调用方拿这批 id 再过一次 ListByIDs 即可（它带 deleted_at IS NULL），
// 这条路径服务两个用途：授权回显（角色 / 用户分权树）与权限点删除前的引用计数。
func (m *MenuModel) ListMenuIDsByPermissionCodes(ctx context.Context, codes []string) ([]uint64, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var ids []uint64
	err := m.db.WithContext(ctx).Table(tableNameSysMenuPermission).
		Where("permission_code IN ?", codes).
		Distinct("menu_id").
		Order("menu_id ASC").
		Pluck("menu_id", &ids).Error
	return ids, err
}

// CreateWithPermissionCodes 在同一事务内写入菜单行与它的权限码集合。
//
// 为什么必须同事务：菜单行写进去、权限码没写，表现是「菜单在列表里、授权树里勾不出来」——
// 半截状态不报错，要靠人工比对两张表才发现（AGENTS.md「写操作的事务与回滚」）。
//
// 旧列 permission_code 由本方法留空、由 UpdateWithPermissionCodes 保持原值：
// 它自 470 起只作 seed 的写入通道与幂等判据（详见 menuUpdateColumns 的注释）。
func (m *MenuModel) CreateWithPermissionCodes(ctx context.Context, e *MenuEntity, codes []string) error {
	unique := DedupePermissionCodes(codes)
	// 旧列留空（不要写首码）：它只服务 seed 的判据，界面一旦写它就会让判据漂移（见 menuUpdateColumns）。
	e.PermissionCode = nil
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return replaceMenuPermissionCodes(tx, e.ID, unique)
	})
}

// UpdateWithPermissionCodes 在同一事务内更新菜单行并全量替换其权限码集合。
//
// **全量替换语义**：传进来的 codes 就是该菜单的完整权限集合，没列的一律撤销。
// 空集合是合法提交（目录 / iframe / 外链本来就不允许绑码），不当成「没改」。
func (m *MenuModel) UpdateWithPermissionCodes(ctx context.Context, e *MenuEntity, codes []string) error {
	unique := DedupePermissionCodes(codes)
	// 旧列保持库里原值：不进 menuUpdateColumns 的字段不会被 SET，也不会触发同步触发器。
	// 这里显式把实体上的值清掉，避免调用方（service 传进来的 entity）带着一个过期的码
	// 在别处被误当成真源。
	e.PermissionCode = nil
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&MenuEntity{}).Where("id = ?", e.ID).
			Select(menuUpdateColumns).Updates(e).Error; err != nil {
			return err
		}
		return replaceMenuPermissionCodes(tx, e.ID, unique)
	})
}

// SoftDeleteWithPermissionCodes 软删菜单并清掉它们的权限码关联行。
//
// 不清关联行的后果：已软删菜单的码仍会被反查命中 —— 授权回显里冒出一个已删菜单的勾选项，
// 权限点的引用计数永久虚高（于是那个权限点再也删不掉）。软删与清关联同事务，失败整体回滚。
func (m *MenuModel) SoftDeleteWithPermissionCodes(ctx context.Context, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var affected int64
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&MenuEntity{}).Where("id IN ? AND deleted_at IS NULL", ids).Update("deleted_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		if affected == 0 {
			return nil
		}
		return tx.Exec("DELETE FROM "+tableNameSysMenuPermission+" WHERE menu_id IN ?", ids).Error
	})
	return affected, err
}

// menuPermissionRow 是 sys_menu_permission 的写入形状。
//
// **故意不带主键字段**：id 由 BIGSERIAL 生成，结构体里放一个零值 ID 会让 gorm 显式写 0，
// 既覆盖默认值又打乱序列。
type menuPermissionRow struct {
	MenuID         uint64    `gorm:"column:menu_id"`
	PermissionCode string    `gorm:"column:permission_code"`
	CreateTime     time.Time `gorm:"column:create_time"`
	UpdateTime     time.Time `gorm:"column:update_time"`
}

// replaceMenuPermissionCodes 全量替换一个菜单的权限码集合（DELETE + INSERT）。
// 调用方保证在事务内 —— 它自己不开事务，因为「先删后插」中间态不可见是调用方的责任。
func replaceMenuPermissionCodes(tx *gorm.DB, menuID uint64, codes []string) error {
	if err := tx.Exec("DELETE FROM "+tableNameSysMenuPermission+" WHERE menu_id = ?", menuID).Error; err != nil {
		return err
	}
	if len(codes) == 0 {
		return nil
	}
	now := time.Now()
	rows := make([]menuPermissionRow, 0, len(codes))
	for _, code := range codes {
		rows = append(rows, menuPermissionRow{MenuID: menuID, PermissionCode: code, CreateTime: now, UpdateTime: now})
	}
	return tx.Table(tableNameSysMenuPermission).Create(&rows).Error
}

// DedupePermissionCodes 去重并保序，顺带丢掉空串。
//
// 唯一索引 (menu_id, permission_code) 本来也会拦住重复，但在这里挡住更诚实：
// 重复提交只是表单里的一个多余勾选，不该变成一次写入失败。
// 导出是因为 service 侧的绑定校验要用同一份判据（它需要先知道「去重后到底有几个码」
// 才能决定「菜单必须至少绑一个」是否满足）—— 两份去重实现迟早会分叉。
func DedupePermissionCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

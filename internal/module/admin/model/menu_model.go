package adminmodel

import (
	"context"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

const tableNameSysMenus = "sys_menus"

const tableNameSysMenuPermission = "sys_menu_permission"

const (
	MenuTypeDirectory = 1
	MenuTypeMenu      = 2
	MenuTypeButton    = 3
	MenuTypeIframe    = 4
	MenuTypeExternal  = 5
)

const (
	MenuStatusDisabled = 0
	MenuStatusEnabled  = 1
)

type MenuEntity struct {
	ID uint64 `gorm:"column:id;primaryKey"`

	PermissionCode *string `gorm:"column:permission_code"`

	PermissionCodes []string `gorm:"-"`
	Title           string   `gorm:"column:title"`
	TitleKey        *string  `gorm:"column:title_key"`
	ParentID        uint64   `gorm:"column:parent_id;default:0"`
	Type            int      `gorm:"column:type;default:2"`
	Path            string   `gorm:"column:path"`
	ExternalURL     string   `gorm:"column:external_url"`
	Icon            string   `gorm:"column:icon"`

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

func (MenuEntity) TableName() string {
	return tableNameSysMenus
}

type MenuModel struct {
	db *gorm.DB
}

func NewMenuModel(db *gorm.DB) *MenuModel {
	return &MenuModel{db: db}
}

func (m *MenuModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&MenuEntity{})
}

func (e *MenuEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

func (e *MenuEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

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

type MenuParentOption struct {
	ID        uint64
	ParentID  uint64
	Title     string
	Type      int
	SortOrder int
}

type MenuPageRow struct {
	MenuEntity

	HasChildren bool `gorm:"-"`

	Matched bool `gorm:"-"`
}

const menuRootFilter = `deleted_at IS NULL AND (parent_id = 0 OR NOT EXISTS (
				SELECT 1 FROM sys_menus p
				WHERE p.id = sys_menus.parent_id AND p.deleted_at IS NULL
			))`

func (m *MenuModel) ListMenuRootsPage(ctx context.Context, page, limit int) (rows []MenuPageRow, total int64, err error) {
	if limit < 1 {
		limit = 1
	}
	if page < 1 {
		page = 1
	}
	if err = m.DB(ctx).Where(menuRootFilter).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total > 0 && int64(page-1) > (total-1)/int64(limit) {
		page = int((total-1)/int64(limit)) + 1
	}
	var roots []MenuEntity
	if err = m.DB(ctx).Where(menuRootFilter).Order("sort_order ASC, id ASC").
		Offset((page - 1) * limit).Limit(limit).Find(&roots).Error; err != nil {
		return nil, 0, err
	}
	all, err := m.menuTreeDescendants(ctx, roots)
	if err != nil {
		return nil, 0, err
	}
	rows = menuRowsOf(all, nil)
	if err = m.fillRowPermissionCodes(ctx, rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

const menuTreeMaxDepth = 8

func (m *MenuModel) menuTreeDescendants(ctx context.Context, roots []MenuEntity) ([]MenuEntity, error) {
	all := make([]MenuEntity, 0, len(roots))
	seen := make(map[uint64]struct{}, len(roots))
	layer := make([]uint64, 0, len(roots))
	for _, r := range roots {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		all = append(all, r)
		layer = append(layer, r.ID)
	}
	for depth := 0; depth < menuTreeMaxDepth && len(layer) > 0; depth++ {
		var children []MenuEntity
		if err := m.DB(ctx).Where("deleted_at IS NULL AND parent_id IN ?", layer).
			Order("sort_order ASC, id ASC").Find(&children).Error; err != nil {
			return nil, err
		}
		next := make([]uint64, 0, len(children))
		for _, c := range children {
			if _, ok := seen[c.ID]; ok {
				continue
			}
			seen[c.ID] = struct{}{}
			all = append(all, c)
			next = append(next, c.ID)
		}
		layer = next
	}
	return all, nil
}

func (m *MenuModel) ListSubtreeIDs(ctx context.Context, rootID uint64) (map[uint64]bool, error) {
	if rootID == 0 {
		return map[uint64]bool{}, nil
	}
	root, err := m.GetByID(ctx, rootID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return map[uint64]bool{}, nil
	}
	nodes, err := m.menuTreeDescendants(ctx, []MenuEntity{*root})
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]bool, len(nodes))
	for _, n := range nodes {
		out[n.ID] = true
	}
	return out, nil
}

func (m *MenuModel) ListMenuSearchForest(ctx context.Context, keyword string) ([]MenuPageRow, error) {
	pattern := "%" + database.EscapeLikePattern(keyword) + "%"
	perm := m.db.WithContext(ctx).Table(tableNameSysMenuPermission).Select("menu_id").
		Where(`permission_code LIKE ? ESCAPE '\'`, pattern)
	var picked []MenuEntity
	if err := m.DB(ctx).Where("deleted_at IS NULL").
		Where(`(title LIKE ? ESCAPE '\' OR path LIKE ? ESCAPE '\' OR remark LIKE ? ESCAPE '\' OR id IN (?))`,
			pattern, pattern, pattern, perm).
		Order("sort_order ASC, id ASC").Find(&picked).Error; err != nil {
		return nil, err
	}
	matched := make(map[uint64]bool, len(picked))
	all := make([]MenuEntity, 0, len(picked))
	seen := make(map[uint64]struct{}, len(picked))

	layer := make([]uint64, 0, len(picked))
	for _, p := range picked {
		matched[p.ID] = true
		if _, ok := seen[p.ID]; ok {
			continue
		}
		seen[p.ID] = struct{}{}
		all = append(all, p)
		if p.ParentID != 0 {
			layer = append(layer, p.ParentID)
		}
	}

	for steps := 0; steps < menuTreeMaxDepth && len(layer) > 0; steps++ {
		uniq := make([]uint64, 0, len(layer))
		for _, id := range layer {
			if _, ok := seen[id]; ok {
				continue
			}
			uniq = append(uniq, id)
		}
		if len(uniq) == 0 {
			break
		}
		var parents []MenuEntity
		if err := m.DB(ctx).Where("deleted_at IS NULL AND id IN ?", uniq).
			Order("sort_order ASC, id ASC").Find(&parents).Error; err != nil {
			return nil, err
		}
		layer = layer[:0]
		for _, p := range parents {
			if _, ok := seen[p.ID]; ok {
				continue
			}
			seen[p.ID] = struct{}{}
			all = append(all, p)
			if p.ParentID != 0 {
				layer = append(layer, p.ParentID)
			}
		}
	}
	rows := menuRowsOf(all, matched)
	if err := m.fillRowPermissionCodes(ctx, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func menuRowsOf(all []MenuEntity, matched map[uint64]bool) []MenuPageRow {
	hasChild := make(map[uint64]struct{}, len(all))
	for _, m := range all {
		if m.ParentID != 0 {
			hasChild[m.ParentID] = struct{}{}
		}
	}
	out := make([]MenuPageRow, 0, len(all))
	for i := range all {
		_, has := hasChild[all[i].ID]
		out = append(out, MenuPageRow{
			MenuEntity:  all[i],
			HasChildren: has,
			Matched:     matched != nil && matched[all[i].ID],
		})
	}
	return out
}

func (m *MenuModel) fillRowPermissionCodes(ctx context.Context, rows []MenuPageRow) error {
	if len(rows) == 0 {
		return nil
	}
	entities := make([]MenuEntity, len(rows))
	for i := range rows {
		entities[i] = rows[i].MenuEntity
	}
	if err := m.fillPermissionCodes(ctx, entities); err != nil {
		return err
	}
	for i := range rows {
		rows[i].MenuEntity = entities[i]
	}
	return nil
}

func (m *MenuModel) ListParentOptions(ctx context.Context) ([]MenuParentOption, error) {
	var options []MenuParentOption
	err := m.DB(ctx).Select("id, parent_id, title, type, sort_order").
		Where("deleted_at IS NULL").Order("sort_order ASC, id ASC").Find(&options).Error
	return options, err
}

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

func (m *MenuModel) ListByIDs(ctx context.Context, ids []uint64) ([]MenuEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []MenuEntity
	err := m.DB(ctx).Where("id IN ? AND deleted_at IS NULL", ids).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

var menuUpdateColumns = []string{
	"title",
	"title_key",
	"parent_id",
	"type",
	"path",
	"external_url",
	"icon",
	"status",
	"is_hidden",
	"is_public",
	"sort_order",
	"remark",
	"update_time",
}

func (m *MenuModel) CountByParentID(ctx context.Context, parentID uint64) (int64, error) {
	var count int64
	err := m.DB(ctx).Where("parent_id = ? AND deleted_at IS NULL", parentID).Count(&count).Error
	return count, err
}

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

func (m *MenuModel) CreateWithPermissionCodes(ctx context.Context, e *MenuEntity, codes []string) error {
	unique := DedupePermissionCodes(codes)

	e.PermissionCode = nil
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return replaceMenuPermissionCodes(tx, e.ID, unique)
	})
}

func (m *MenuModel) UpdateWithPermissionCodes(ctx context.Context, e *MenuEntity, codes []string) error {
	unique := DedupePermissionCodes(codes)

	e.PermissionCode = nil
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&MenuEntity{}).Where("id = ?", e.ID).
			Select(menuUpdateColumns).Updates(e).Error; err != nil {
			return err
		}
		return replaceMenuPermissionCodes(tx, e.ID, unique)
	})
}

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
		return tx.Table(tableNameSysMenuPermission).Where("menu_id IN ?", ids).Delete(nil).Error
	})
	return affected, err
}

type menuPermissionRow struct {
	MenuID         uint64    `gorm:"column:menu_id"`
	PermissionCode string    `gorm:"column:permission_code"`
	CreateTime     time.Time `gorm:"column:create_time"`
	UpdateTime     time.Time `gorm:"column:update_time"`
}

func replaceMenuPermissionCodes(tx *gorm.DB, menuID uint64, codes []string) error {
	if err := tx.Table(tableNameSysMenuPermission).Where("menu_id = ?", menuID).Delete(nil).Error; err != nil {
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

package i18n

// admin.go — sys_i18n 词条的后台读写端口（审计 I18N-003）。
//
// 此前词条只能靠迁移 seed：改一句文案要写一个迁移文件、重跑迁移、重新部署。
// 结果是运营侧实际上改不动文案 —— 能改的人只有写代码的人。
//
// 本文件只做三件事：列出（带筛选）/ 保存 / 删除。职责边界：
//   - 迁移 seed 是**默认值来源**（DO NOTHING，不覆盖人工修改）；
//   - 后台是**真相来源**（人工改过之后，seed 不再回来覆盖）。
//
// 保存成功后主动 LoadCache()：否则改了词条要等下一次定时刷新才生效，
// 运营会以为「保存失败」然后反复保存 —— 而每次保存都推进 revision、
// 标记全站页面待重建，代价并不小。

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 列表上限：词条总量可达数万级（070 的记录），一次全取会拖垮页面。
const (
	I18nListDefaultLimit = 100
	I18nListMaxLimit     = 500
)

// 词条读写错误（调用方按 errors.Is 分类，文案可直接展示给运营）。
var (
	// ErrI18nUnavailable 数据库未初始化 / 句柄不可用。
	ErrI18nUnavailable = errors.New("词条存储不可用")
	// ErrI18nKeyEmpty 词条 key 为空。
	ErrI18nKeyEmpty = errors.New("词条 key 不能为空")
	// ErrI18nLangEmpty 语言为空。
	ErrI18nLangEmpty = errors.New("语言不能为空")
	// ErrI18nValueEmpty 词条内容为空（空内容在构建期等同未命中，写它是纯噪声）。
	ErrI18nValueEmpty = errors.New("词条内容不能为空")
)

// Entry 一条词条（key × lang）。
// Entry 一条词条。
//
// 必须显式写 gorm 列名：sys_i18n 的键与值是 item_key / item_value，而 gorm 默认按字段名
// 推导出 key / value —— 缺映射时 Find 出来的这两列恒为零值，表现为「后台词条页的 key 与
// 内容两列一片空白」，而 Lang / Category / UpdateTime 看着正常（它们的推导名恰好等于列名）。
type Entry struct {
	ID         int64  `gorm:"column:id" json:"id"`
	Key        string `gorm:"column:item_key" json:"key"`
	Lang       string `gorm:"column:lang" json:"lang"`
	Value      string `gorm:"column:item_value" json:"value"`
	Category   string `gorm:"column:category" json:"category"`
	Remark     string `gorm:"column:remark" json:"remark"`
	Status     int16  `gorm:"column:status" json:"status"`
	UpdateTime string `gorm:"column:update_time" json:"updateTime"`
}

// EntryFilter 列表筛选。
type EntryFilter struct {
	// Keyword 同时匹配 key 与内容（运营多半只记得「那句话大概是什么」）。
	Keyword  string
	Lang     string
	Category string
	Limit    int
	Offset   int
}

// i18nAdminDB 取数据库句柄（未初始化返回 ErrI18nUnavailable）。
func i18nAdminDB() (*gorm.DB, error) {
	db, err := database.GetDB()
	if err != nil || db == nil {
		return nil, ErrI18nUnavailable
	}
	return db, nil
}

// ListEntries 列出词条（key 升序，同 key 内按语言）。
//
// 排序固定 key 升序而不是更新时间：运营是「找某一条」而不是「看最近的」，
// 按更新时间排会让同一个 key 的各语言版本散落在不同页。
func ListEntries(ctx context.Context, f EntryFilter) (items []Entry, total int64, err error) {
	db, err := i18nAdminDB()
	if err != nil {
		return nil, 0, err
	}
	q := db.WithContext(ctx).Table("sys_i18n")
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		q = q.Where("item_key LIKE ? OR item_value LIKE ?", like, like)
	}
	if lang := strings.TrimSpace(f.Lang); lang != "" {
		q = q.Where("lang = ?", lang)
	}
	if cat := strings.TrimSpace(f.Category); cat != "" {
		q = q.Where("category = ?", cat)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = I18nListDefaultLimit
	}
	if limit > I18nListMaxLimit {
		limit = I18nListMaxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	err = q.Order("item_key ASC, lang ASC").Limit(limit).Offset(f.Offset).Find(&items).Error
	return items, total, err
}

// GetEntry reads one exact (key, lang) pair without using a paginated list.
func GetEntry(ctx context.Context, key, lang string) (entry *Entry, err error) {
	key, lang = strings.TrimSpace(key), strings.TrimSpace(lang)
	if key == "" {
		return nil, ErrI18nKeyEmpty
	}
	if lang == "" {
		return nil, ErrI18nLangEmpty
	}
	db, err := i18nAdminDB()
	if err != nil {
		return nil, err
	}
	var row Entry
	result := db.WithContext(ctx).Table("sys_i18n").Where("item_key = ? AND lang = ?", key, lang).Take(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	return &row, nil
}

// UpdateEntry updates an existing exact pair; an absent entry never becomes a new row.
func UpdateEntry(ctx context.Context, e Entry) (err error) {
	key, lang := strings.TrimSpace(e.Key), strings.TrimSpace(e.Lang)
	if key == "" {
		return ErrI18nKeyEmpty
	}
	if lang == "" {
		return ErrI18nLangEmpty
	}
	if strings.TrimSpace(e.Value) == "" {
		return ErrI18nValueEmpty
	}
	db, err := i18nAdminDB()
	if err != nil {
		return err
	}
	result := db.WithContext(ctx).Table("sys_i18n").Where("item_key = ? AND lang = ?", key, lang).
		Updates(map[string]any{"item_value": e.Value, "category": strings.TrimSpace(e.Category),
			"remark": strings.TrimSpace(e.Remark), "update_time": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return reloadCacheAfterWrite()
}

// Categories 列出已用到的分类（筛选下拉用，去重后有序）。
func Categories(ctx context.Context) (out []string, err error) {
	db, err := i18nAdminDB()
	if err != nil {
		return nil, err
	}
	var rows []string
	if err = db.WithContext(ctx).Table("sys_i18n").
		Distinct().Where("category <> ''").Pluck("category", &rows).Error; err != nil {
		return nil, err
	}
	sort.Strings(rows)
	return rows, nil
}

// SaveEntry 保存（新增或更新）一条词条，随后重载缓存。
func SaveEntry(ctx context.Context, e Entry) (err error) {
	db, err := i18nAdminDB()
	if err != nil {
		return err
	}
	key := strings.TrimSpace(e.Key)
	lang := strings.TrimSpace(e.Lang)
	if key == "" {
		return ErrI18nKeyEmpty
	}
	if lang == "" {
		return ErrI18nLangEmpty
	}
	if strings.TrimSpace(e.Value) == "" {
		return ErrI18nValueEmpty
	}
	status := e.Status
	if status == 0 {
		status = 1 // 默认启用：运营新建词条时不该再想「为什么没生效」
	}
	row := map[string]any{
		"item_key":    key,
		"lang":        lang,
		"item_value":  e.Value,
		"category":    strings.TrimSpace(e.Category),
		"remark":      strings.TrimSpace(e.Remark),
		"status":      status,
		"http_code":   200,
		"update_time": time.Now(),
	}
	// upsert 的冲突目标与 055/066 的唯一约束一致（item_key, lang）。
	if err = db.WithContext(ctx).Table("sys_i18n").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "item_key"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"item_value", "category", "remark", "status", "update_time"}),
	}).Create(row).Error; err != nil {
		return err
	}
	return reloadCacheAfterWrite()
}

// DeleteEntry 删除一条词条，随后重载缓存。
//
// 删掉之后构建期回退到组件包内的中文兜底 —— 这是可见的降级（不是空白），
// 所以允许删除而不做「不许删」的保护；但要在返回值里让调用方知道后果。
func DeleteEntry(ctx context.Context, key, lang string) (err error) {
	db, err := i18nAdminDB()
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(lang) == "" {
		return ErrI18nKeyEmpty
	}
	if err = db.WithContext(ctx).Table("sys_i18n").
		Where("item_key = ? AND lang = ?", strings.TrimSpace(key), strings.TrimSpace(lang)).
		Delete(nil).Error; err != nil {
		return err
	}
	return reloadCacheAfterWrite()
}

// reloadCacheAfterWrite 写库后立即重载内存缓存。
//
// 失败不返回错误：词条已经落库，缓存晚几十秒生效是可以接受的降级；
// 把缓存失败当成保存失败会让运营重复保存（而每次保存都推进 revision）。
func reloadCacheAfterWrite() error {
	if err := LoadCache(); err != nil {
		return fmt.Errorf("词条已保存，但缓存重载失败（%w）：稍后会自动刷新", err)
	}
	return nil
}

// escapeLike 转义 LIKE 元字符（与媒体库同一约定：用户输入不当通配符用）。
func escapeLike(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return r.Replace(s)
}

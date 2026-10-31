package i18n

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

const (
	I18nListDefaultLimit = 100
	I18nListMaxLimit     = 500
)

var (
	ErrI18nUnavailable = errors.New("词条存储不可用")

	ErrI18nKeyEmpty = errors.New("词条 key 不能为空")

	ErrI18nLangEmpty = errors.New("语言不能为空")

	ErrI18nValueEmpty = errors.New("词条内容不能为空")
)

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

type EntryFilter struct {
	Keyword  string
	Lang     string
	Category string
	Limit    int
	Offset   int
}

func i18nAdminDB() (*gorm.DB, error) {
	db, err := database.GetDB()
	if err != nil || db == nil {
		return nil, ErrI18nUnavailable
	}
	return db, nil
}

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

func CountByLang(ctx context.Context, lang string) (int64, error) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return 0, ErrI18nLangEmpty
	}
	db, err := i18nAdminDB()
	if err != nil {
		return 0, err
	}
	var n int64
	if err = db.WithContext(ctx).Table("sys_i18n").
		Where("lang = ? AND status = 1", lang).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

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
		status = 1
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

	if err = db.WithContext(ctx).Table("sys_i18n").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "item_key"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"item_value", "category", "remark", "status", "update_time"}),
	}).Create(row).Error; err != nil {
		return err
	}
	return reloadCacheAfterWrite()
}

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

func reloadCacheAfterWrite() error {
	if err := LoadCache(); err != nil {
		return fmt.Errorf("词条已保存，但缓存重载失败（%w）：稍后会自动刷新", err)
	}
	return nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return r.Replace(s)
}

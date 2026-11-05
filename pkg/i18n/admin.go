package i18n

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
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

	// ErrI18nSelectionEmpty 是「批量删除时一个词条都没选」。
	ErrI18nSelectionEmpty = errors.New("没有选中要删除的词条")
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

// EntryRef 指向一条词条：(key, lang) 唯一确定 sys_i18n 里的一行。
type EntryRef struct {
	Key  string
	Lang string
}

// MissingEntry 是一条「要删但库里不存在」的词条。
//
// 它携带原始的 Key / Lang 而不是只留一句错误文本：handler 需要把它们放进
// data.items 让用户能定位是哪条没删成。
type MissingEntry struct {
	Key  string
	Lang string
}

// MissingEntriesError 表示批量删除里至少有一条不存在。
//
// 为什么不用 pkg/facing 的错误形状：pkg/i18n 是被 pkg/response 依赖的基础设施，
// 反过来 import pkg/facing 会把「错误声明 + 状态码」这条业务语义拖进词库层。
// 这里只交「哪几条不存在」这个事实，状态码与文案由 controller 转成自己的声明。
type MissingEntriesError struct {
	Items []MissingEntry
}

func (e *MissingEntriesError) Error() string {
	return "批量删除的 " + strconv.Itoa(len(e.Items)) + " 条词条不存在"
}

// DeleteEntries 批量删除词条：要么全删掉，要么一条都不删。
//
// 事务语义（逐条 DeleteEntry 做不到的）：
//   - 先做参数校验，任何一条 key/lang 为空即整批拒绝，不进事务；
//   - 事务内按 (key, lang) 升序 FOR UPDATE 锁住这批行 —— 与 DeleteEntry 单条路径
//     以及保存路径排同一个队，避免「校验时在、删的时候没了」的窗口；
//   - 锁回的行数必须等于请求条数，少一条就整批回滚并报 MissingEntriesError；
//   - 删除按**锁到的行 id** 精确下刀，不按 item_key：同一 key 的多语言是不同行，
//     按 key 删会连带删掉用户没勾的语言；
//   - 删除与锁在同一事务，行锁保证这中间没有别的连接插进来；
//   - 缓存重载只在提交之后做一次：事务回滚时缓存还是旧值，本来就不需要刷。
//
// 空选择在这里也直接拒绝：调用方不该把「什么都没选」当成「删了 0 条」的成功。
func DeleteEntries(ctx context.Context, refs []EntryRef) (deleted int, err error) {
	db, err := i18nAdminDB()
	if err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, ErrI18nSelectionEmpty
	}

	normalized := make([]EntryRef, 0, len(refs))
	seen := make(map[EntryRef]struct{}, len(refs))
	for _, ref := range refs {
		key := strings.TrimSpace(ref.Key)
		lang := strings.TrimSpace(ref.Lang)
		if key == "" {
			return 0, ErrI18nKeyEmpty
		}
		if lang == "" {
			return 0, ErrI18nLangEmpty
		}
		item := EntryRef{Key: key, Lang: lang}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Key != normalized[j].Key {
			return normalized[i].Key < normalized[j].Key
		}
		return normalized[i].Lang < normalized[j].Lang
	})

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 锁：按 (key, lang) 升序逐条 SELECT ... FOR UPDATE。
		// 一条查询锁不完「(key, lang) 组合」这批行，所以按排序后的顺序逐条锁 ——
		// 顺序固定，两个并发的批量删除才不会互相等成死锁。
		//
		// 锁到的 **行 id** 是后面删除的唯一依据。不能退回成「按 item_key IN (...) 删」：
		// 同一个 key 在三种语言下各有一行，用户只勾了两条，按 key 删会把没勾的
		// 那条一起删掉 —— 行数等式在有别的 key 命中时会露馅，但语义上已经错了。
		lockedIDs := make([]int64, 0, len(normalized))
		locked := make(map[EntryRef]struct{}, len(normalized))
		for _, item := range normalized {
			var ids []int64
			if lerr := tx.Table("sys_i18n").
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("item_key = ? AND lang = ?", item.Key, item.Lang).
				Pluck("id", &ids).Error; lerr != nil {
				return lerr
			}
			if len(ids) == 0 {
				continue
			}
			locked[item] = struct{}{}
			lockedIDs = append(lockedIDs, ids...)
		}

		if len(locked) != len(normalized) {
			missing := make([]MissingEntry, 0, len(normalized)-len(locked))
			for _, item := range normalized {
				if _, ok := locked[item]; ok {
					continue
				}
				missing = append(missing, MissingEntry{Key: item.Key, Lang: item.Lang})
			}
			// 返回错误让事务回滚：部分存在也不删，全成全败。
			return &MissingEntriesError{Items: missing}
		}

		res := tx.Table("sys_i18n").Where("id IN ?", lockedIDs).Delete(nil)
		if res.Error != nil {
			return res.Error
		}
		// 受影响行数必须与「锁住的那批行」逐条对上：多删或少删都是缺陷，整批回滚。
		if res.RowsAffected != int64(len(lockedIDs)) {
			return ErrI18nUnavailable
		}
		deleted = int(res.RowsAffected)
		return nil
	})
	if err != nil {
		return 0, err
	}

	// 缓存刷新放在提交之后：事务里刷新会把「还没提交的值」写进缓存。
	return deleted, reloadCacheAfterWrite()
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

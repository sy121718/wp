package sysconfigmodel

// sys_dict_model.go — 数据字典（sys_dict）与行政区划（sys_area）的**最小读取能力**。
//
// 这两张表是「系统级基础数据」（语言 / 货币 / 国家），由迁移建表并 seed，没有工程归属、
// 也不在 RLS 策略对象里。本批只做**读**：系统设置页的下拉选项取它们。
// 完整的字典 CRUD 与后台字典页是下一批的事 —— 现在就做会把「给下拉供数」这件事
// 裹进一套还没想清楚的编辑语义里（谁能不能改、改了要不要重建站点）。
//
// 放本模块而不是新建 sysdict 模块：消费方只有系统设置页，而设置页已经在本模块的
// 域里（sys_config）。等字典有了自己的编辑界面与生命周期，再拆模块不迟。

import (
	"context"
)

const (
	tableNameSysDict = "sys_dict"
	tableNameSysArea = "sys_area"
)

// 字典类型（sys_dict.type 的取值，与迁移 seed 一致）。
const (
	// DictTypeLanguage 语言（code 形如 zh-CN；url_code 是 URL 里的短码）。
	DictTypeLanguage = "language"
	// DictTypeCurrency 货币（code 是 ISO 4217 三字母码）。
	DictTypeCurrency = "currency"
)

// AreaKindCountry 行政区划类型：国家。
const AreaKindCountry = "country"

// SysDictEntity sys_dict 一行（只映射本批要用的列）。
//
// 不映射 iso639_1 / minor_unit 之类：读的时候用不到，映射进来只会多一处需要跟着
// 迁移改的地方。列的真源是建表迁移。
type SysDictEntity struct {
	Type    string `gorm:"column:type;primaryKey"`
	Code    string `gorm:"column:code;primaryKey"`
	URLCode string `gorm:"column:url_code"`
	Symbol  string `gorm:"column:symbol"`
	Enabled bool   `gorm:"column:enabled"`
	// UIAvailable 界面译文是否已有（sys_dict 的既有列，此前零消费方）。
	//
	// 含义只有这一个：**这种语言的界面文案已有人译过**。它不表示「语言可用」
	// （那是 enabled），也不表示「内容已翻译」。i18n 词条页用它标记选项。
	UIAvailable bool `gorm:"column:ui_available"`
	SortOrder   int  `gorm:"column:sort_order"`
}

// TableName 表名。
func (SysDictEntity) TableName() string { return tableNameSysDict }

// SysAreaEntity sys_area 一行（只映射本批要用的列）。
type SysAreaEntity struct {
	Code            string `gorm:"column:code;primaryKey"`
	Kind            string `gorm:"column:kind"`
	NameZh          string `gorm:"column:name_zh"`
	NameEn          string `gorm:"column:name_en"`
	DefaultLang     string `gorm:"column:default_lang"`
	DefaultCurrency string `gorm:"column:default_currency"`
	Enabled         bool   `gorm:"column:enabled"`
	SortOrder       int    `gorm:"column:sort_order"`
}

// TableName 表名。
func (SysAreaEntity) TableName() string { return tableNameSysArea }

// ListDictItems 列出某类型的**启用**字典项（sort_order 升序、同序按 code，输出稳定）。
//
// 只列 enabled：停用项留在表里是「曾经有过」，给下拉供数时它不该出现 ——
// 让运营能选到一个已经停用的语言，等于把「停用」这件事变成摆设。
func (m *Model) ListDictItems(ctx context.Context, dictType string) (rows []SysDictEntity, err error) {
	err = m.db.WithContext(ctx).Table(tableNameSysDict).
		Where("type = ? AND enabled = true", dictType).
		Order("sort_order ASC, code ASC").Scan(&rows).Error
	return rows, err
}

// ListAreasByKind 列出某类型的**启用**行政区划（同上理由）。
func (m *Model) ListAreasByKind(ctx context.Context, kind string) (rows []SysAreaEntity, err error) {
	err = m.db.WithContext(ctx).Table(tableNameSysArea).
		Where("kind = ? AND enabled = true", kind).
		Order("sort_order ASC, code ASC").Scan(&rows).Error
	return rows, err
}

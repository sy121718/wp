package sysconfigmodel

import (
	"context"
)

const (
	tableNameSysDict = "sys_dict"
	tableNameSysArea = "sys_area"
)

const (
	DictTypeLanguage = "language"

	DictTypeCurrency = "currency"
)

const AreaKindCountry = "country"

type SysDictEntity struct {
	Type    string `gorm:"column:type;primaryKey"`
	Code    string `gorm:"column:code;primaryKey"`
	URLCode string `gorm:"column:url_code"`
	Symbol  string `gorm:"column:symbol"`
	Enabled bool   `gorm:"column:enabled"`

	UIAvailable bool `gorm:"column:ui_available"`
	SortOrder   int  `gorm:"column:sort_order"`
}

func (SysDictEntity) TableName() string { return tableNameSysDict }

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

func (SysAreaEntity) TableName() string { return tableNameSysArea }

func (m *Model) ListDictItems(ctx context.Context, dictType string) (rows []SysDictEntity, err error) {
	err = m.db.WithContext(ctx).Table(tableNameSysDict).
		Where("type = ? AND enabled = true", dictType).
		Order("sort_order ASC, code ASC").Scan(&rows).Error
	return rows, err
}

func (m *Model) ListAreasByKind(ctx context.Context, kind string) (rows []SysAreaEntity, err error) {
	err = m.db.WithContext(ctx).Table(tableNameSysArea).
		Where("kind = ? AND enabled = true", kind).
		Order("sort_order ASC, code ASC").Scan(&rows).Error
	return rows, err
}

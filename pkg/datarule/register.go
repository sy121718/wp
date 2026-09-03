// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），
// 在 GORM Query 回调中自动注入行级数据过滤条件（WHERE 子句与 Omit 字段）。
package datarule

import (
	"context"
	"fmt"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

// 确保 import 正确
var _ context.Context

// ruleProvider 全局数据规则提供者实例，在模块初始化时通过 SetProvider 设置。
var ruleProvider RuleProvider

// SetProvider 设置全局数据规则提供者，必须在注册插件之前调用。
func SetProvider(provider RuleProvider) {
	ruleProvider = provider
}

// GetProvider 获取当前已设置的全局规则提供者实例。
func GetProvider() RuleProvider {
	return ruleProvider
}

// RegisterPlugin 将 DataRulePlugin 注册到全局数据库实例的 GORM 回调链中。
// 需要先通过 SetProvider 设置规则提供者，否则返回错误。
func RegisterPlugin() error {
	if ruleProvider == nil {
		return fmt.Errorf("datarule RuleProvider 未设置，无法注册插件")
	}

	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("获取数据库实例失败: %w", err)
	}

	return RegisterPluginWithDB(db)
}

// RegisterPluginWithDB 将 DataRulePlugin 注册到指定的数据库实例的 GORM 回调链中。
// 适用于需要将数据权限插件绑定到特定 DB 实例的场景。
func RegisterPluginWithDB(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("数据库实例为 nil")
	}
	return db.Use(NewDataRulePlugin(ruleProvider))
}

package datarule

import (
	"context"
	"fmt"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

var _ context.Context

var ruleProvider RuleProvider

func SetProvider(provider RuleProvider) {
	ruleProvider = provider
}

func GetProvider() RuleProvider {
	return ruleProvider
}

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

func RegisterPluginWithDB(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("数据库实例为 nil")
	}
	return db.Use(NewDataRulePlugin(ruleProvider))
}

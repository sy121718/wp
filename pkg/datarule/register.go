// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），把「谁能看、谁能改」下沉到 GORM 回调链。
//
// 覆盖范围（读写两路，改这个包之前请先读完这段）：
//
//   - Query（读）：行级读保护 —— 注入 WHERE；字段级读屏蔽 —— 注入 Omits，
//     GORM 在 SELECT 语义下把它解释为「不查询该列」。
//   - Create（写）：值校验。Create 没有 WHERE 可以注入，语义是 CASL 式的
//     「检查将要创建的对象」：从 db.Statement.Dest 反射取值，逐条与条件组比对，
//     不满足即拒绝落库（批量创建逐元素校验）。**这是行为变更** ——
//     在此之前 Create 完全不受数据权限约束。求值失败（字段在 Dest 上取不到、
//     类型不认识、Dest 形状不支持）一律 fail-closed 拒绝，绝不静默放过。
//   - Update（写）：行级写保护 —— 注入与 Query **完全相同**的行条件（与 Query
//     共用同一份条件构造，不另写一套，否则两套语义会漂移）；字段级写屏蔽 ——
//     复用同一份 OmitFields，GORM 在 UPDATE 语义下把 Omits 解释为「不更新该列」。
//     语句执行后影响行数为 0 时返回明确错误（原因与方言边界见 beforeUpdate / afterUpdate 的注释）。
//   - Delete（写）：行级写保护 —— 同样复用 Query 的行条件构造；执行后 0 行同样报错。
//     DELETE 没有字段概念，因此不注入 Omits。
//
// 规则取不到（provider 报错）→ 直接 AddError 并终止该回调（与 Query 路一致）；
// 规则集为空表示「该用户在该域没有任何限制」→ 放行，这是既有语义。
//
// **Query 之外的路径没有任何兜底**：本插件只在 GORM 的 Query / Create / Update / Delete
// 回调链上生效。裸 SQL（db.Raw / db.Exec）、未经 context 传入 UserContext 的调用
// （GetUserContext 返回 nil）、以及**未注册数据域的表**，全部不经过这里 —— 它们在数据权限
// 意义上等于「不受约束」。要保护一张表：先在本包注册它的数据域，再保证写它的语句走 GORM
// 回调链且 context 里带着 UserContext。
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

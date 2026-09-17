package adminmodel

import "go_wp/pkg/datarule"

// AdminDataRuleDomain 返回 admin 模块注册到 datarule 引擎的数据域声明。
//
// 白名单由 AdminEntity 字段上的 datarule tag 派生，表名来自 AdminEntity.TableName()：
// 谁拥有这张表，谁声明它的可配置字段 —— 域的类型在 pkg/datarule，具体的域值在这里，
// 注册动作由模块装配入口（inbound/http 的 bootstrapDataRule）在启动时执行。
func AdminDataRuleDomain() (datarule.DomainConfig, error) {
	return datarule.DomainFromEntity("ADMIN", "管理员", AdminEntity{})
}

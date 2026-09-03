// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），
// 在 GORM Query 回调中自动注入行级数据过滤条件（WHERE 子句与 Omit 字段）。
package datarule

import (
	"context"
	"regexp"
	"strings"
)

// registeredDomains 已注册的数据域集合，key 为业务域标识，value 为域配置信息。
var registeredDomains = make(map[string]DomainConfig)

// RegisterDomain 将指定的数据域配置注册到全局映射表中，供后续查询时匹配表名使用。
func RegisterDomain(cfg DomainConfig) {
	registeredDomains[cfg.Domain] = cfg
}

// GetRegisteredDomains 返回所有已注册的数据域配置快照列表。
func GetRegisteredDomains() []DomainConfig {
	result := make([]DomainConfig, 0, len(registeredDomains))
	for _, cfg := range registeredDomains {
		result = append(result, cfg)
	}
	return result
}

// UserContextKey context.Context 中存储 UserContext 的键类型。
// 定义为空 struct 类型以确保 key 的唯一性，避免与其他 context value 冲突。
type UserContextKey struct{}

// GetUserContext 从 context.Context 中提取用户身份上下文。
// 如果 context 为 nil、未设置或类型不匹配，返回 nil。
func GetUserContext(ctx context.Context) *UserContext {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(UserContextKey{})
	if v == nil {
		return nil
	}
	uc, ok := v.(*UserContext)
	if !ok {
		return nil
	}
	return uc
}

// supportedOps 数据权限规则支持的操作符白名单。
// 不在此集合中的操作符在构建条件时会被忽略。
var supportedOps = map[string]bool{
	"EQ": true, "NEQ": true, "GT": true, "GTE": true, "LT": true, "LTE": true,
	"IN": true, "NOT_IN": true, "LIKE": true, "NOT_LIKE": true, "BETWEEN": true,
}

// validOp 检查操作符是否在 supportedOps 白名单中，不区分大小写。
func validOp(op string) bool {
	return supportedOps[strings.ToUpper(op)]
}

// deptScopeRe 匹配 dept.scope:* 引用表达式的正则。
// 格式：dept.scope:<SCOPE>[:<extra>]
// 支持的范围值：SELF（本人所在部门）、SELF_AND_CHILDREN（本人部门及子部门）、CUSTOM（自定义）、ALL（全部）。
var deptScopeRe = regexp.MustCompile(`^dept\.scope:(SELF|SELF_AND_CHILDREN|CUSTOM|ALL)(?::(.+))?$`)

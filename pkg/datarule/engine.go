// Package datarule 提供基于 GORM 插件的数据权限控制能力。
// 通过注册数据域（Domain）与规则提供者（RuleProvider），
// 在 GORM Query 回调中自动注入行级数据过滤条件（WHERE 子句与 Omit 字段）。
package datarule

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// registeredDomains 已注册的数据域集合，key 为业务域标识，value 为域配置信息。
// 装配期写入、请求期读取（GetRules 每查询一次都读），读写一律经 registeredDomainMu。
var (
	registeredDomainMu sync.RWMutex
	registeredDomains  = make(map[string]DomainConfig)
)

// RegisterDomain 校验并注册数据域配置，供后续查询时按表名匹配。
//
// 校验只针对「错了会静默失效」的项：域标识与表名为空会让 resolveDomain 永不命中，
// 该表的行级过滤随之静默失效（beforeQuery fail-open）；白名单里的非法操作符会让条件
// 被静默丢弃。所以这里一律 fail-fast，由装配入口决定如何处理（本项目在启动阶段直接失败）。
func RegisterDomain(cfg DomainConfig) error {
	if err := validateDomainConfig(cfg); err != nil {
		return err
	}
	registeredDomainMu.Lock()
	defer registeredDomainMu.Unlock()
	registeredDomains[cfg.Domain] = cfg
	return nil
}

// GetDomain 按业务域标识返回已注册的域配置。
func GetDomain(domain string) (DomainConfig, bool) {
	registeredDomainMu.RLock()
	defer registeredDomainMu.RUnlock()
	cfg, ok := registeredDomains[domain]
	return cfg, ok
}

// GetRegisteredDomains 返回所有已注册的数据域配置快照列表。
func GetRegisteredDomains() []DomainConfig {
	registeredDomainMu.RLock()
	defer registeredDomainMu.RUnlock()
	result := make([]DomainConfig, 0, len(registeredDomains))
	for _, cfg := range registeredDomains {
		result = append(result, cfg)
	}
	return result
}

// validateDomainConfig 校验域声明的必填项与白名单字段定义。
// WhiteList 允许为空（该域没有任何可配置字段是合法状态），DomainLabel 仅用于展示。
func validateDomainConfig(cfg DomainConfig) error {
	if strings.TrimSpace(cfg.Domain) == "" {
		return errors.New("datarule: 数据域标识不能为空")
	}
	if strings.TrimSpace(cfg.TableName) == "" {
		return fmt.Errorf("datarule: 数据域 %s 的表名不能为空", cfg.Domain)
	}
	declared := make(map[string]struct{}, len(cfg.WhiteList))
	for _, field := range cfg.WhiteList {
		if escapeField(field.Field) == "" {
			return fmt.Errorf("datarule: 数据域 %s 的字段名 %q 非法（只允许字母、数字与下划线）", cfg.Domain, field.Field)
		}
		if strings.TrimSpace(field.Label) == "" {
			return fmt.Errorf("datarule: 数据域 %s 的字段 %s 缺少中文标签", cfg.Domain, field.Field)
		}
		if len(field.Operators) == 0 {
			return fmt.Errorf("datarule: 数据域 %s 的字段 %s 未声明可用操作符", cfg.Domain, field.Field)
		}
		for _, op := range field.Operators {
			if !IsSupportedOp(op) {
				return fmt.Errorf("datarule: 数据域 %s 的字段 %s 使用了不支持的操作符 %q（支持：%s）",
					cfg.Domain, field.Field, op, strings.Join(Operators, ", "))
			}
		}
		if _, dup := declared[field.Field]; dup {
			return fmt.Errorf("datarule: 数据域 %s 的字段 %s 重复声明", cfg.Domain, field.Field)
		}
		declared[field.Field] = struct{}{}
	}
	return nil
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

// Operators 引擎支持的全部操作符，顺序即管理端下拉展示的推荐顺序。
// 域白名单（FieldDef.Operators）只能取这里的子集，tag 声明与规则校验都以此为准。
var Operators = []string{
	"EQ", "NEQ", "GT", "GTE", "LT", "LTE",
	"IN", "NOT_IN", "LIKE", "NOT_LIKE", "BETWEEN",
}

// supportedOps 数据权限规则支持的操作符白名单。
// 不在此集合中的操作符在构建条件时会被忽略。
var supportedOps = func() map[string]bool {
	ops := make(map[string]bool, len(Operators))
	for _, op := range Operators {
		ops[op] = true
	}
	return ops
}()

// IsSupportedOp 报告操作符是否为引擎支持（大小写与首尾空白不敏感）。
func IsSupportedOp(op string) bool {
	return supportedOps[strings.ToUpper(strings.TrimSpace(op))]
}

// validOp 检查操作符是否在 supportedOps 白名单中，不区分大小写。
func validOp(op string) bool {
	return IsSupportedOp(op)
}

// deptScopeRe 匹配 dept.scope:* 引用表达式的正则。
// 格式：dept.scope:<SCOPE>[:<extra>]
// 支持的范围值：SELF（本人所在部门）、SELF_AND_CHILDREN（本人部门及子部门）、CUSTOM（自定义）、ALL（全部）。
var deptScopeRe = regexp.MustCompile(`^dept\.scope:(SELF|SELF_AND_CHILDREN|CUSTOM|ALL)(?::(.+))?$`)

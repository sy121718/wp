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
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// registeredDomains 已注册数据域的**不可变快照**（key 为业务域标识，value 为域配置）。
//
// **读写一律经 domainsSnapshot()（atomic.Pointer 全量替换）**：注册只发生在装配期
// （罕见），而读发生在**每一条** GORM 语句上 —— resolveDomain（plugin.go）是热路径，
// 每条语句都要按表名匹配域。所以这里用「不可变快照 + 原子替换」：读侧零锁零竞争，
// 克隆在写侧做、并由 registerMu 串行（防并发注册丢更新）。与 pkg/casbin 的
// urlCodeMap 是同一套手法。
//
// 此前这里是「裸 map + sync.RWMutex」，但 resolveDomain 直接遍历 map **没有加锁** ——
// 注释声称「读写一律经 registeredDomainMu」而实现并非如此，同时形式上是一处 map
// 并发读写（Go 里可能直接 fatal）。现已收口：**不要绕过 domainsSnapshot() 直接读**。
var (
	registeredDomains atomic.Pointer[map[string]DomainConfig]
	registerMu        sync.Mutex // 只保护写侧的「克隆 + 替换」；读侧完全不经过它
)

func init() {
	empty := make(map[string]DomainConfig)
	registeredDomains.Store(&empty)
}

// domainsSnapshot 返回当前域快照；**调用方只读、不得修改**（改了会污染其它 goroutine）。
func domainsSnapshot() map[string]DomainConfig {
	return *registeredDomains.Load()
}

// RegisterDomain 校验并注册数据域配置，供后续查询时按表名匹配。
//
// 校验只针对「错了会静默失效」的项：域标识与表名为空会让 resolveDomain 永不命中，
// 该表的行级过滤随之静默失效（beforeQuery fail-open）；白名单里的非法操作符会让条件
// 被静默丢弃。所以这里一律 fail-fast，由装配入口决定如何处理（本项目在启动阶段直接失败）。
func RegisterDomain(cfg DomainConfig) error {
	if err := validateDomainConfig(cfg); err != nil {
		return err
	}
	registerMu.Lock()
	defer registerMu.Unlock()
	current := domainsSnapshot()
	next := make(map[string]DomainConfig, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[cfg.Domain] = cfg
	registeredDomains.Store(&next)
	return nil
}

// GetDomain 按业务域标识返回已注册的域配置。
func GetDomain(domain string) (DomainConfig, bool) {
	cfg, ok := domainsSnapshot()[domain]
	return cfg, ok
}

// GetRegisteredDomains 返回所有已注册的数据域配置快照列表。
func GetRegisteredDomains() []DomainConfig {
	snapshot := domainsSnapshot()
	result := make([]DomainConfig, 0, len(snapshot))
	for _, cfg := range snapshot {
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

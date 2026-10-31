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

var (
	registeredDomains atomic.Pointer[map[string]DomainConfig]
	registerMu        sync.Mutex
)

func init() {
	empty := make(map[string]DomainConfig)
	registeredDomains.Store(&empty)
}

func domainsSnapshot() map[string]DomainConfig {
	return *registeredDomains.Load()
}

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

func GetDomain(domain string) (DomainConfig, bool) {
	cfg, ok := domainsSnapshot()[domain]
	return cfg, ok
}

func GetRegisteredDomains() []DomainConfig {
	snapshot := domainsSnapshot()
	result := make([]DomainConfig, 0, len(snapshot))
	for _, cfg := range snapshot {
		result = append(result, cfg)
	}
	return result
}

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

type UserContextKey struct{}

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

var Operators = []string{
	"EQ", "NEQ", "GT", "GTE", "LT", "LTE",
	"IN", "NOT_IN", "LIKE", "NOT_LIKE", "BETWEEN",
}

var supportedOps = func() map[string]bool {
	ops := make(map[string]bool, len(Operators))
	for _, op := range Operators {
		ops[op] = true
	}
	return ops
}()

func IsSupportedOp(op string) bool {
	return supportedOps[strings.ToUpper(strings.TrimSpace(op))]
}

func validOp(op string) bool {
	return IsSupportedOp(op)
}

var deptScopeRe = regexp.MustCompile(`^dept\.scope:(SELF|SELF_AND_CHILDREN|CUSTOM|ALL)(?::(.+))?$`)

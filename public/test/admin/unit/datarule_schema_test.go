// 数据域白名单与真实表结构的回归网。
//
// 白名单字段名同时是「配置界面能选什么」与「引擎注入 Omit/条件时用的列名」，
// 名字漂了不会立刻报错：域照样命中、规则照样落库，直到某次查询才以
// column does not exist 或「Omit 了一个不存在的列、什么也没屏蔽」的形式暴露。
// 这里把白名单钉在真实表结构上：字段来自 AdminEntity 的 datarule tag（唯一来源），
// 列名拿 information_schema 逐个核对。
package unit

import (
	"context"
	"testing"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

// TestDataRuleWhiteListMatchesRealColumns ADMIN 域白名单的每个字段都必须是 sys_admin 的真实列。
func TestDataRuleWhiteListMatchesRealColumns(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	domain, ok := datarule.GetDomain("ADMIN")
	if !ok {
		t.Fatalf("ADMIN 域未注册")
	}
	if len(domain.WhiteList) == 0 {
		t.Fatalf("ADMIN 域白名单为空：AdminEntity 的 datarule tag 丢了？")
	}
	if domain.TableName != (adminmodel.AdminEntity{}).TableName() {
		t.Fatalf("域表名与实体表名不一致: 域=%s 实体=%s", domain.TableName, (adminmodel.AdminEntity{}).TableName())
	}

	columns := realColumns(t, ctx, e, domain.TableName)
	for _, field := range domain.WhiteList {
		if !columns[field.Field] {
			t.Errorf("白名单字段 %s(%s) 在表 %s 里不存在", field.Field, field.Label, domain.TableName)
		}
		for _, op := range field.Operators {
			if !datarule.IsSupportedOp(op) {
				t.Errorf("字段 %s 声明了引擎不支持的操作符 %s", field.Field, op)
			}
		}
	}
}

// TestDataRuleWhiteListExcludesSecrets 敏感列不进白名单：password 是真实列但绝不可配。
// 这条断言的是「白名单不是照着表结构抄的，而是挑出来的」—— 一旦有人图省事做成全列开放，这里会红。
func TestDataRuleWhiteListExcludesSecrets(t *testing.T) {
	e := setupEnv(t)
	ctx := context.Background()

	domain, _ := datarule.GetDomain("ADMIN")
	columns := realColumns(t, ctx, e, domain.TableName)
	if !columns["password"] {
		t.Fatalf("前置条件不成立：sys_admin 应有 password 列")
	}

	for _, field := range domain.WhiteList {
		if field.Field == "password" {
			t.Fatalf("password 不允许进入数据权限白名单")
		}
	}
}

// realColumns 返回指定表在当前 schema 下的真实列名集合。
func realColumns(t *testing.T, ctx context.Context, e *env, table string) map[string]bool {
	t.Helper()

	var names []string
	if err := e.db.WithContext(ctx).
		Raw("SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ?", table).
		Scan(&names).Error; err != nil {
		t.Fatalf("查询 %s 列失败: %v", table, err)
	}
	if len(names) == 0 {
		t.Fatalf("表 %s 在当前 schema 不存在", table)
	}

	result := make(map[string]bool, len(names))
	for _, name := range names {
		result[name] = true
	}
	return result
}

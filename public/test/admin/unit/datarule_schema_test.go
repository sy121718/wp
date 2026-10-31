package unit

import (
	"context"
	"testing"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
)

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

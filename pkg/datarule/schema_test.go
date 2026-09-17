package datarule

import (
	"strings"
	"testing"
)

// probeEntity tag 声明的正例。
type probeEntity struct {
	ID       uint64 `gorm:"column:id;primaryKey"`
	Username string `gorm:"column:username" datarule:"label=用户名;ops=EQ,NEQ,LIKE"`
	DeptID   uint64 `gorm:"column:dept_id" datarule:"label=所属部门;ops=eq,NEQ"`
	Remark   string `gorm:"column:remark"`
}

func (probeEntity) TableName() string { return "probe_table" }

// noTableName 未实现 TableName()：表名不能猜，必须由实体显式给出。
type noTableName struct {
	Field string `gorm:"column:field" datarule:"label=字段;ops=EQ"`
}

func TestDomainFromEntity(t *testing.T) {
	cfg, err := DomainFromEntity("PROBE", "探针", probeEntity{})
	if err != nil {
		t.Fatalf("生成域失败: %v", err)
	}
	if cfg.TableName != "probe_table" || cfg.DomainLabel != "探针" {
		t.Fatalf("域基本信息不符: %+v", cfg)
	}
	// 没有 datarule tag 的字段不进白名单（fail-closed）
	if len(cfg.WhiteList) != 2 {
		t.Fatalf("白名单应只含声明过的字段: %+v", cfg.WhiteList)
	}
	if cfg.WhiteList[0].Field != "username" || cfg.WhiteList[0].Label != "用户名" {
		t.Fatalf("字段声明不符: %+v", cfg.WhiteList[0])
	}
	// 小写操作符归一化为大写，重复项去重
	if got := strings.Join(cfg.WhiteList[1].Operators, ","); got != "EQ,NEQ" {
		t.Fatalf("操作符应归一化去重: %s", got)
	}

	// 注册的域应能被 GetDomain 取回
	if err := RegisterDomain(cfg); err != nil {
		t.Fatalf("注册域失败: %v", err)
	}
	if got, ok := GetDomain("PROBE"); !ok || got.TableName != "probe_table" {
		t.Fatalf("GetDomain 未取回注册结果: %+v ok=%v", got, ok)
	}
}

// TestDomainFromEntityRejects 声明写错一律报错 —— 不允许白名单静默降级。
func TestDomainFromEntityRejects(t *testing.T) {
	cases := []struct {
		name   string
		entity any
		want   string
	}{
		{"缺少 TableName()", noTableName{}, "必须实现 TableName()"},
		{"实体为 nil", nil, "实体为 nil"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DomainFromEntity("X", "X", c.entity)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("期望错误包含 %q，实际: %v", c.want, err)
			}
		})
	}
}

func TestParseEntityFieldTag(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr string
	}{
		{"label=用户名;ops=EQ", ""},
		{"label=用户名", "缺少 ops"},
		{"ops=EQ", "缺少 label"},
		{"label=用户名;ops=CONTAINS", "不支持的操作符"},
		{"label=用户名;ops=EQ;dtype=varchar", "未知键"},
		{"label=用户名;ops", "缺少 ="},
	}
	for _, c := range cases {
		_, _, err := parseEntityFieldTag(c.raw)
		if c.wantErr == "" {
			if err != nil {
				t.Fatalf("tag %q 期望通过，实际: %v", c.raw, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Fatalf("tag %q 期望错误包含 %q，实际: %v", c.raw, c.wantErr, err)
		}
	}
}

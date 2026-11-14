package adminhttp

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newFormContext(t *testing.T, form url.Values) *gin.Context {
	t.Helper()
	req := httptest.NewRequest("POST", "/admin/datarules/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestDataruleConfigFromForm(t *testing.T) {
	form := url.Values{}
	form.Set("domain", "ADMIN")
	form.Add("omit_fields", "phone")
	form.Add("omit_fields", "email")
	// 线上是静态平行数组：组状态一组一个，行字段按 DOM 顺序重复提交。
	form.Add("groupLogic", "OR")
	form.Add("condGroup", "0")
	form.Add("condField", "username")
	form.Add("condOp", "LIKE")
	form.Add("condValue", "admin")
	form.Add("condGroup", "0")
	form.Add("condField", "status")
	form.Add("condOp", "IN")
	form.Add("condValue", "1,2")
	form.Add("groupLogic", "AND")
	form.Add("condGroup", "1")
	form.Add("condField", "dept_id")
	form.Add("condOp", "EQ")
	form.Add("condValue", "dept.scope:SELF")

	cfg, err := dataruleConfigFromForm(newFormContext(t, form))
	if err != nil {
		t.Fatalf("合法表单不应配对失败：%v", err)
	}

	if len(cfg.OmitFields) != 2 || cfg.OmitFields[0] != "phone" || cfg.OmitFields[1] != "email" {
		t.Fatalf("屏蔽字段解析不符: %+v", cfg.OmitFields)
	}
	if len(cfg.ConditionGroups) != 2 {
		t.Fatalf("应解析出 2 个条件组: %+v", cfg.ConditionGroups)
	}
	if cfg.ConditionGroups[0].Logic != "OR" || len(cfg.ConditionGroups[0].Conditions) != 2 {
		t.Fatalf("条件组 0 不符: %+v", cfg.ConditionGroups[0])
	}
	if got := cfg.ConditionGroups[0].Conditions[1]; got.Field != "status" || got.Op != "IN" || got.Value != "1,2" {
		t.Fatalf("条件组 0 第二行不符: %+v", got)
	}
	if got := cfg.ConditionGroups[1].Conditions[0]; got.Field != "dept_id" || got.Value != "dept.scope:SELF" {
		t.Fatalf("条件组 1 不符: %+v", got)
	}
}

func TestDataruleConfigFromFormKeepsEmptyRows(t *testing.T) {
	form := url.Values{}
	form.Set("domain", "ADMIN")
	form.Add("groupLogic", "AND")
	form.Add("condGroup", "0")
	form.Add("condField", "username")
	form.Add("condOp", "EQ")
	form.Add("condValue", "")
	form.Add("condGroup", "0")
	form.Add("condField", "")
	form.Add("condOp", "")
	form.Add("condValue", "")

	cfg, err := dataruleConfigFromForm(newFormContext(t, form))
	if err != nil {
		t.Fatalf("合法表单不应配对失败：%v", err)
	}
	if len(cfg.ConditionGroups) != 1 || len(cfg.ConditionGroups[0].Conditions) != 2 {
		t.Fatalf("解析阶段应保留 2 行（含空行）: %+v", cfg.ConditionGroups)
	}

	dropped := dataruleDropEmptyGroups(cfg)
	if len(dropped.ConditionGroups) != 0 {
		t.Fatalf("没有有效条件的组应被丢弃: %+v", dropped.ConditionGroups)
	}
}

func TestDataruleConfigFromFormDeletedRowReindexes(t *testing.T) {
	form := url.Values{}
	form.Set("domain", "ADMIN")
	form.Add("groupLogic", "AND")

	form.Add("condGroup", "0")
	form.Add("condField", "username")
	form.Add("condOp", "EQ")
	form.Add("condValue", "a")
	form.Add("condGroup", "0")
	form.Add("condField", "status")
	form.Add("condOp", "EQ")
	form.Add("condValue", "1")

	cfg, err := dataruleConfigFromForm(newFormContext(t, form))
	if err != nil {
		t.Fatalf("合法表单不应配对失败：%v", err)
	}
	rows := cfg.ConditionGroups[0].Conditions
	if len(rows) != 2 || rows[0].Field != "username" || rows[1].Field != "status" {
		t.Fatalf("行序应按屏幕顺序还原: %+v", rows)
	}
}

// 行数组缺列：配不齐的条件必须整份拒绝，不能落成「空条件」再被保存。
func TestDataruleConfigFromFormRejectsMismatchedRows(t *testing.T) {
	form := url.Values{}
	form.Add("groupLogic", "AND")
	form.Add("condGroup", "0")
	form.Add("condField", "username")
	form.Add("condOp", "EQ")
	// 故意少一个 condValue
	if _, err := dataruleConfigFromForm(newFormContext(t, form)); err == nil {
		t.Fatal("行数组长度不一致必须拒绝")
	}
}

// 非法组号（非数 / 负数 / 越界）都必须拒绝，且不按请求数字无限扩组。
func TestDataruleConfigFromFormRejectsBadGroupIndex(t *testing.T) {
	for name, group := range map[string]string{
		"非数": "x", "负数": "-1", "越界": "5",
	} {
		t.Run(name, func(t *testing.T) {
			form := url.Values{}
			form.Add("groupLogic", "AND")
			form.Add("condGroup", group)
			form.Add("condField", "username")
			form.Add("condOp", "EQ")
			form.Add("condValue", "a")
			if _, err := dataruleConfigFromForm(newFormContext(t, form)); err == nil {
				t.Fatalf("组号 %q 必须拒绝", group)
			}
		})
	}
}

// 没有条件行时允许没有组状态：这是「清空配置」的合法路径。
func TestDataruleConfigFromFormAllowsEmptyClear(t *testing.T) {
	cfg, err := dataruleConfigFromForm(newFormContext(t, url.Values{}))
	if err != nil {
		t.Fatalf("清空配置不应被当成畸形输入：%v", err)
	}
	if len(cfg.ConditionGroups) != 0 {
		t.Fatalf("清空后不应有组：%+v", cfg.ConditionGroups)
	}
}

// 多组多行不得串位。
func TestDataruleConfigFromFormKeepsGroupRowsAligned(t *testing.T) {
	form := url.Values{}
	form.Add("groupLogic", "AND")
	form.Add("groupLogic", "OR")
	for _, row := range [][3]string{
		{"0", "username", "a"}, {"0", "status", "1"}, {"1", "dept_id", "d"},
	} {
		form.Add("condGroup", row[0])
		form.Add("condField", row[1])
		form.Add("condOp", "EQ")
		form.Add("condValue", row[2])
	}
	cfg, err := dataruleConfigFromForm(newFormContext(t, form))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ConditionGroups) != 2 || len(cfg.ConditionGroups[0].Conditions) != 2 || len(cfg.ConditionGroups[1].Conditions) != 1 {
		t.Fatalf("分组行数不符：%+v", cfg.ConditionGroups)
	}
	if got := cfg.ConditionGroups[0].Conditions[1].Field; got != "status" {
		t.Fatalf("组内行序串位：%q", got)
	}
	if got := cfg.ConditionGroups[1].Conditions[0].Field; got != "dept_id" {
		t.Fatalf("组间串位：%q", got)
	}
}

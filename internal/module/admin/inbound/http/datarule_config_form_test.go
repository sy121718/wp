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
	req := httptest.NewRequest("POST", "/admin/datarules/config-editor", strings.NewReader(form.Encode()))
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
	form.Set("groups[0].logic", "OR")
	form.Add("groups[0].conditions[0].field", "username")
	form.Add("groups[0].conditions[0].op", "LIKE")
	form.Add("groups[0].conditions[0].value", "admin")
	form.Add("groups[0].conditions[1].field", "status")
	form.Add("groups[0].conditions[1].op", "IN")
	form.Add("groups[0].conditions[1].value", "1,2")
	form.Set("groups[1].logic", "AND")
	form.Add("groups[1].conditions[0].field", "dept_id")
	form.Add("groups[1].conditions[0].op", "EQ")
	form.Add("groups[1].conditions[0].value", "dept.scope:SELF")

	cfg := dataruleConfigFromForm(newFormContext(t, form))

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
	form.Set("groups[0].logic", "AND")
	form.Add("groups[0].conditions[0].field", "username")
	form.Add("groups[0].conditions[0].op", "EQ")
	form.Add("groups[0].conditions[0].value", "")
	form.Add("groups[0].conditions[1].field", "")
	form.Add("groups[0].conditions[1].op", "")
	form.Add("groups[0].conditions[1].value", "")

	cfg := dataruleConfigFromForm(newFormContext(t, form))
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
	form.Set("groups[0].logic", "AND")

	form.Add("groups[0].conditions[0].field", "username")
	form.Add("groups[0].conditions[0].op", "EQ")
	form.Add("groups[0].conditions[0].value", "a")
	form.Add("groups[0].conditions[1].field", "status")
	form.Add("groups[0].conditions[1].op", "EQ")
	form.Add("groups[0].conditions[1].value", "1")

	cfg := dataruleConfigFromForm(newFormContext(t, form))
	rows := cfg.ConditionGroups[0].Conditions
	if len(rows) != 2 || rows[0].Field != "username" || rows[1].Field != "status" {
		t.Fatalf("行序应按屏幕顺序还原: %+v", rows)
	}
}

package contenttemplate

import (
	"context"
	"testing"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
)

const entityTemplatesTestProjectID = "proj-1"

type ctScopeStub struct {
	contenttemplatecontract.ContentTemplateService
	lastList      *contenttemplatedto.ListReq
	lastScopedPj  string
	lastScopedTyp string
}

func (s *ctScopeStub) List(_ context.Context, req *contenttemplatedto.ListReq) ([]*contenttemplatedto.TemplateResp, error) {
	s.lastList = req
	return []*contenttemplatedto.TemplateResp{{ID: "t1", Name: "本工程详情模板"}}, nil
}

func (s *ctScopeStub) ResolveTemplateScoped(_ context.Context, projectID, entityType string) (*contenttemplatecontract.ResolvedTemplate, error) {
	s.lastScopedPj = projectID
	s.lastScopedTyp = entityType
	return &contenttemplatecontract.ResolvedTemplate{TemplateID: "t1"}, nil
}

func TestEntityTemplatesCarriesProjectScope(t *testing.T) {
	stub := &ctScopeStub{}
	rows, err := New(stub).EntityTemplates(context.Background(), entityTemplatesTestProjectID, "product")
	if err != nil {
		t.Fatalf("读模板清单失败: %v", err)
	}
	if stub.lastList == nil {
		t.Fatal("List 一次都没被调用")
	}
	if stub.lastList.ProjectID != entityTemplatesTestProjectID {
		t.Fatalf("ListReq.ProjectID = %q，want %q —— 留空时内容模板侧在多工程部署下回 ErrProjectRequired，"+
			"调用方按降级处理，模板卡片区静默变空", stub.lastList.ProjectID, entityTemplatesTestProjectID)
	}
	if stub.lastList.EntityType != "product" {
		t.Fatalf("ListReq.EntityType = %q，want %q", stub.lastList.EntityType, "product")
	}
	if len(rows) != 1 || rows[0].Name != "本工程详情模板" {
		t.Fatalf("适配器应把对方的行翻译成渲染视图，实际 %+v", rows)
	}
}

func TestDefaultEntityTemplateCarriesProjectScope(t *testing.T) {
	stub := &ctScopeStub{}
	id, err := New(stub).DefaultEntityTemplate(context.Background(), entityTemplatesTestProjectID, "product")
	if err != nil {
		t.Fatalf("解析默认模板失败: %v", err)
	}
	if stub.lastScopedPj != entityTemplatesTestProjectID || stub.lastScopedTyp != "product" {
		t.Fatalf("ResolveTemplateScoped 收到 (projectID=%q, entityType=%q)，want (%q, %q) —— "+
			"工程留空时多工程部署下解析必然失败，当前模板 id 永远回填不出来",
			stub.lastScopedPj, stub.lastScopedTyp, entityTemplatesTestProjectID, "product")
	}
	if id != "t1" {
		t.Fatalf("默认模板 id = %q，want %q", id, "t1")
	}
}

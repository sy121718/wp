package productservice

// 模板业务查询仅转交端口事实；工程范围校验与页面渲染分别验证。

import (
	"context"
	"errors"
	"net/http"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	projectdto "go_wp/internal/module/project/dto"
)

// detailTemplatePortStub 记录各类模板事实的查询调用。
type detailTemplatePortStub struct {
	calls []string
	err   error
}

func (p *detailTemplatePortStub) DetailTemplateCandidates(context.Context, string, string) ([]productcontract.DetailTemplateCandidateView, error) {
	p.calls = append(p.calls, "Candidates")
	return nil, p.err
}

func (p *detailTemplatePortStub) DefaultDetailTemplate(context.Context, string, string) (string, error) {
	p.calls = append(p.calls, "Default")
	return "", p.err
}

func (p *detailTemplatePortStub) BoundDetailTemplate(context.Context, string, string, string) (productcontract.BoundTemplateView, error) {
	p.calls = append(p.calls, "Bound")
	return productcontract.BoundTemplateView{}, p.err
}

func (p *detailTemplatePortStub) DetailTemplatePanel(context.Context, string, string, string) (productcontract.DetailTemplatePanelFacts, error) {
	p.calls = append(p.calls, "Panel")
	return productcontract.DetailTemplatePanelFacts{}, p.err
}

// newDetailTemplateReadModel 读模型：只接工程清单与模板端口。
func newDetailTemplateReadModel(projects []projectdto.ProjectResp, projectErr error, port productcontract.DetailTemplatePort) (*Service, *productPageProjectStub) {
	ps := productPageProjectStub{rows: projects, err: projectErr}
	s, _, _ := newProductPageReadModel(ps)
	if port != nil {
		s.SetDetailTemplatePort(port)
	}
	return s, &ps
}

func TestDetailTemplateQueriesReturnUnmodifiedFacts(t *testing.T) {
	port := &detailTemplatePortStub{}
	s, _ := newDetailTemplateReadModel([]projectdto.ProjectResp{{ID: "p1"}}, nil, port)
	candidates, err := s.QueryDetailTemplates(context.Background(), "p1")
	if err != nil || candidates == nil {
		t.Fatalf("候选空集应显式返回：%v %v", candidates, err)
	}
	s.QueryDefaultDetailTemplate(context.Background(), "p1")
	s.QueryBoundDetailTemplate(context.Background(), "p1", "prod-1")
	if len(port.calls) != 3 {
		t.Fatalf("各查询只调用对应端口：%v", port.calls)
	}
}
func TestDetailTemplateScopeRejectsUnknownProjectBeforeQueries(t *testing.T) {
	port := &detailTemplatePortStub{}
	s, _ := newDetailTemplateReadModel([]projectdto.ProjectResp{{ID: "p1"}}, nil, port)
	scope, err := s.ResolveCatalogScope(context.Background(), "does-not-exist")
	wantPageFailure(t, "未知工程", scope, err, http.StatusBadRequest)
	if len(port.calls) != 0 {
		t.Fatalf("工程失败不应触发模板查询：%v", port.calls)
	}
}
func TestDetailTemplateScopePropagatesProjectFailure(t *testing.T) {
	port := &detailTemplatePortStub{}
	s, _ := newDetailTemplateReadModel(nil, errors.New("projects unavailable"), port)
	scope, err := s.ResolveCatalogScope(context.Background(), "")
	wantPageFailure(t, "工程清单读不到", scope, err, http.StatusInternalServerError)
	if len(port.calls) != 0 {
		t.Fatalf("工程失败不应触发模板查询：%v", port.calls)
	}
}

func TestDetailTemplateQueriesPropagatePortFailures(t *testing.T) {
	failure := errors.New("storage unavailable")
	s, _ := newDetailTemplateReadModel(nil, nil, &detailTemplatePortStub{err: failure})
	queries := []func() error{
		func() error { _, err := s.QueryDetailTemplates(context.Background(), "p1"); return err },
		func() error { _, err := s.QueryDefaultDetailTemplate(context.Background(), "p1"); return err },
		func() error { _, err := s.QueryBoundDetailTemplate(context.Background(), "p1", "prod-1"); return err },
		func() error { _, err := s.QueryDetailTemplatePanel(context.Background(), "p1", "prod-1"); return err },
	}
	for i, query := range queries {
		if err := query(); !errors.Is(err, failure) {
			t.Fatalf("查询 %d 吞掉读取错误：%v", i, err)
		}
	}
}

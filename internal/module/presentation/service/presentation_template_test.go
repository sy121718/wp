package presentationservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"

	presentationenums "go_wp/internal/module/presentation/enums"
	"gorm.io/gorm"
)

type stubTemplateService struct {
	meta       *contenttemplatedto.TemplateResp
	metaErr    error
	resolved   *contenttemplatecontract.ResolvedTemplate
	resolveErr error
	byIDErr    error

	activateErr   error
	activateCalls int
}

func (s *stubTemplateService) Create(context.Context, *contenttemplatedto.CreateReq) (*contenttemplatedto.TemplateResp, error) {
	return nil, nil
}

func (s *stubTemplateService) Update(context.Context, *contenttemplatedto.UpdateReq) (*contenttemplatedto.TemplateResp, error) {
	return nil, nil
}

func (s *stubTemplateService) Activate(context.Context, *contenttemplatedto.ActivateReq) (*contenttemplatedto.TemplateResp, error) {
	s.activateCalls++
	return s.meta, s.activateErr
}

func (s *stubTemplateService) Get(context.Context, *contenttemplatedto.GetReq) (*contenttemplatedto.TemplateResp, error) {
	return s.meta, s.metaErr
}

func (s *stubTemplateService) List(context.Context, *contenttemplatedto.ListReq) ([]*contenttemplatedto.TemplateResp, error) {
	return nil, nil
}

func (s *stubTemplateService) Delete(context.Context, *contenttemplatedto.DeleteReq) error {
	return nil
}

func (s *stubTemplateService) BulkDelete(context.Context, *contenttemplatedto.BulkDeleteReq) error {
	return nil
}

func (s *stubTemplateService) Impact(context.Context, *contenttemplatedto.ImpactReq) (*contenttemplatedto.ImpactResp, error) {
	return &contenttemplatedto.ImpactResp{Available: false, References: []contenttemplatedto.TemplateReference{}}, nil
}

func (s *stubTemplateService) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return nil, nil
}

func (s *stubTemplateService) ResolveTemplate(context.Context, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return s.resolved, s.resolveErr
}

func (s *stubTemplateService) ResolveTemplateByRole(context.Context, string, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *stubTemplateService) ResolveTemplateByID(context.Context, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return s.resolved, s.byIDErr
}

func (s *stubTemplateService) GetScoped(context.Context, string, string) (*contenttemplatedto.TemplateResp, error) {
	return s.meta, s.metaErr
}

func (s *stubTemplateService) ResolveTemplateScoped(context.Context, string, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return s.resolved, s.resolveErr
}

func (s *stubTemplateService) ResolveTemplateByRoleScoped(context.Context, string, string, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *stubTemplateService) ResolveTemplateByIDScoped(context.Context, string, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return s.resolved, s.byIDErr
}

func TestResolveTemplateRejectsForeignTypeBeforeParsing(t *testing.T) {

	svc := &Service{templates: &stubTemplateService{
		meta:     &contenttemplatedto.TemplateResp{ID: "tpl-article", EntityType: "article"},
		byIDErr:  errors.New("ErrFieldBindingInvalid: 字段绑定 product.name 不属于 article 数据源"),
		resolved: &contenttemplatecontract.ResolvedTemplate{TemplateID: "tpl-article", EntityType: "article"},
	}}
	_, err := svc.resolveTemplate(context.Background(), "proj-1", "product", "tpl-article")
	if err == nil {
		t.Fatal("跨类型模板应被拒绝")
	}
	if !strings.Contains(err.Error(), presentationenums.ErrTemplateTypeMismatch) {
		t.Fatalf("应报类型不匹配，实际: %v", err)
	}
	if strings.Contains(err.Error(), "字段绑定") {
		t.Fatalf("不应把文档校验细节当成本次失败的原因: %v", err)
	}
}

func TestResolveTemplateSurfacesDocumentErrorForSameType(t *testing.T) {
	bindingErr := errors.New("ErrFieldBindingInvalid: 字段 product.secretField 不在数据源字段白名单内")
	svc := &Service{templates: &stubTemplateService{
		meta:    &contenttemplatedto.TemplateResp{ID: "tpl-product", EntityType: "product"},
		byIDErr: bindingErr,
	}}
	_, err := svc.resolveTemplate(context.Background(), "proj-1", "product", "tpl-product")
	if !errors.Is(err, bindingErr) {
		t.Fatalf("文档校验错误应原样透出（否则排查方向会反），实际: %v", err)
	}
	if strings.Contains(err.Error(), presentationenums.ErrNoTemplate) {
		t.Fatalf("不应被压成「没有可用模板」: %v", err)
	}
}

func TestResolveTemplateSurfacesDefaultTemplateError(t *testing.T) {
	inner := errors.New("ErrFieldBindingInvalid: 字段 product.x 不在数据源字段白名单内")
	svc := &Service{templates: &stubTemplateService{resolveErr: inner}}
	_, err := svc.resolveTemplate(context.Background(), "proj-1", "product", "")
	if !errors.Is(err, inner) {
		t.Fatalf("默认模板的错误也应透出，实际: %v", err)
	}
}

func TestResolveTemplateReturnsResolvedForSameType(t *testing.T) {
	want := &contenttemplatecontract.ResolvedTemplate{TemplateID: "tpl-1", EntityType: "product", Version: 3}
	svc := &Service{templates: &stubTemplateService{
		meta:     &contenttemplatedto.TemplateResp{ID: "tpl-1", EntityType: "product"},
		resolved: want,
	}}
	got, err := svc.resolveTemplate(context.Background(), "proj-1", "product", "tpl-1")
	if err != nil {
		t.Fatalf("同类型模板应解析成功: %v", err)
	}
	if got == nil || got.TemplateID != "tpl-1" || got.Version != 3 {
		t.Fatalf("返回的模板不对: %+v", got)
	}
}

func TestTemplateTypeMismatchIsFalseWhenTemplateMissing(t *testing.T) {
	svc := &Service{templates: &stubTemplateService{metaErr: errors.New("ErrNotFound")}}
	if svc.templateTypeMismatch(context.Background(), "proj-1", "missing", "product") {
		t.Fatal("模板不存在时不应判为类型不匹配")
	}
	svc = &Service{templates: &stubTemplateService{meta: nil}}
	if svc.templateTypeMismatch(context.Background(), "proj-1", "missing", "product") {
		t.Fatal("模板为空时不应判为类型不匹配")
	}
}

func TestResolveTemplateSkipsTypeCheckForBlankID(t *testing.T) {
	resolved := &contenttemplatecontract.ResolvedTemplate{TemplateID: "default", EntityType: "product"}
	svc := &Service{templates: &stubTemplateService{resolved: resolved}}
	got, err := svc.resolveTemplate(context.Background(), "proj-1", "product", "   ")
	if err != nil {
		t.Fatalf("空白 ID 应按默认模板解析: %v", err)
	}
	if got == nil || got.TemplateID != "default" {
		t.Fatalf("应回落到类型默认模板，实际: %+v", got)
	}
}

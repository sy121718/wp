package presentationservice

// presentation_template_test.go — 模板解析的就近单测（审计 CQ-020）。
//
// 这一块是「错误分类」逻辑，没有数据库参与，用 stub 契约就能完整覆盖 ——
// 而它恰恰是最容易在重构中被抹平的地方：把三条失败原因压成同一句话，
// 编译通过、发布也「失败得很有礼貌」，只是运营看不出该去改哪里。
//
// 用例对应三种真实事故：
//   1. 选了别的类型的模板 → 必须报「类型不匹配」，而不是模板文档在那套数据源下的
//      校验细节（后者会把人引向改模板内容，而文档本身没错）；
//   2. 同类型模板的文档越界 → 必须透出原始错误（含哪个字段越界），不能压成「没有可用模板」；
//   3. 完全没有模板 → 同样透出，让运营知道是缺模板还是模板坏了。

import (
	"context"
	"errors"
	"strings"
	"testing"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"

	presentationenums "go_wp/internal/module/presentation/enums"
	"gorm.io/gorm"
)

// stubTemplateService 只实现解析相关方法，其余留空 —— 单测只关心错误分类。
type stubTemplateService struct {
	meta       *contenttemplatedto.TemplateResp
	metaErr    error
	resolved   *contenttemplatecontract.ResolvedTemplate
	resolveErr error
	byIDErr    error
	// activateErr 切换生效模板的可配错误；activateCalls 记录调用次数
	//（呈现侧不消费该能力，但契约加了方法就得有形状一致的实现）。
	activateErr   error
	activateCalls int
}

func (s *stubTemplateService) Create(context.Context, *contenttemplatedto.CreateReq) (*contenttemplatedto.TemplateResp, error) {
	return nil, nil
}

func (s *stubTemplateService) Update(context.Context, *contenttemplatedto.UpdateReq) (*contenttemplatedto.TemplateResp, error) {
	return nil, nil
}

// Activate 切换生效模板（多套存着、单套生效）：呈现侧只做解析，不消费这个能力 ——
// 桩件返回可配错误，供需要模拟"切换失败"的用例使用。
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

// Delete 删除模板：呈现侧的解析链路不消费这个能力（契约随 contenttemplate 的删除权限点一起扩），
// 桩件只要形状对得上 —— 没有它这个包连编译都过不去。
func (s *stubTemplateService) Delete(context.Context, *contenttemplatedto.DeleteReq) error {
	return nil
}

func (s *stubTemplateService) ResolveTemplate(context.Context, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return s.resolved, s.resolveErr
}

// ResolveTemplateByRole 归档模板解析（审计 EDT-004）：桩件没有归档模板，
// 一律按 ErrNotFound —— 与「该实体类型没配归档模板」的真实行为一致。
func (s *stubTemplateService) ResolveTemplateByRole(context.Context, string, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *stubTemplateService) ResolveTemplateByID(context.Context, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	return s.resolved, s.byIDErr
}

// ---- DB-009 第二批：带显式工程作用域的入口（桩件不区分工程，行为与上面一致）----

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

// TestResolveTemplateRejectsForeignTypeBeforeParsing 跨类型模板先判类型，不落到文档校验。
func TestResolveTemplateRejectsForeignTypeBeforeParsing(t *testing.T) {
	// 关键构造：模板文档本身「有问题」（按自己的类型校验也过不了），
	// 但真正的错因是类型选错了 —— 后者的文案才有指导意义。
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

// TestResolveTemplateSurfacesDocumentErrorForSameType 同类型模板的解析错误原样透出。
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

// TestResolveTemplateSurfacesDefaultTemplateError 默认模板解析失败同样透出。
func TestResolveTemplateSurfacesDefaultTemplateError(t *testing.T) {
	inner := errors.New("ErrFieldBindingInvalid: 字段 product.x 不在数据源字段白名单内")
	svc := &Service{templates: &stubTemplateService{resolveErr: inner}}
	_, err := svc.resolveTemplate(context.Background(), "proj-1", "product", "")
	if !errors.Is(err, inner) {
		t.Fatalf("默认模板的错误也应透出，实际: %v", err)
	}
}

// TestResolveTemplateReturnsResolvedForSameType 类型相符时正常返回。
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

// TestTemplateTypeMismatchIsFalseWhenTemplateMissing 模板不存在时不判为类型不匹配。
//
// 「查不到」与「类型不对」是两种失败：前者交给后续解析报「模板已失效」，
// 若这里返回 true，错误会变成「类型不匹配」—— 把人引向换模板，而模板根本没了。
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

// TestResolveTemplateSkipsTypeCheckForBlankID 空白 ID 视为未指定：不走类型判定，按默认模板解析。
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

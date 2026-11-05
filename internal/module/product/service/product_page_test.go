package productservice

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
)

type productPageProjectStub struct {
	projectcontract.ProjectService
	rows []projectdto.ProjectResp
	err  error
}

func (s productPageProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, s.err
}

type productPageWarehousePortStub struct {
	calls int

	err error
}

func (s *productPageWarehousePortStub) WarehouseOptions(context.Context, string) ([]productdto.WarehouseOptionResp, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return []productdto.WarehouseOptionResp{}, nil
}

func (s *productPageWarehousePortStub) WarehouseSKUOptions(context.Context, string, []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error) {
	s.calls++
	return []productdto.WarehouseSKUGroupResp{}, nil
}

type productPageTemplatePortStub struct {
	calls int
}

func (s *productPageTemplatePortStub) EntityTemplates(context.Context, string, string) ([]productdto.EntityTemplateOptionResp, error) {
	s.calls++
	return []productdto.EntityTemplateOptionResp{}, nil
}

func (s *productPageTemplatePortStub) DefaultEntityTemplate(context.Context, string, string) (string, error) {
	s.calls++
	return "", nil
}

func newProductPageReadModel(projects productPageProjectStub) (*Service, *productPageWarehousePortStub, *productPageTemplatePortStub) {
	wh := &productPageWarehousePortStub{}
	tpl := &productPageTemplatePortStub{}
	s := NewService(nil, projects, stubMasterDataChanges{})
	s.SetWarehouseOptionsPort(wh)
	s.SetEntityTemplatePort(tpl)
	return s, wh, tpl
}

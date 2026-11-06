package unit

import (
	"context"

	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
)

// stubTemplateRefs 模拟删除事务里的跨模块引用反查端口。
//
// 它只回答「外部实体是否仍引用这些模板」；模板之间结构槽位绑定由 service
// 自己的表查询负责，因此默认返回空集代表「没有外部引用」。
type stubTemplateRefs struct {
	rows []contenttemplatecontract.TemplateReferenceRow
	err  error
	// seenTx 记录调用方是否把事务句柄真的传了下来（端口不得自开事务）。
	seenTx *gorm.DB
}

func (s *stubTemplateRefs) ListTemplateReferencesTx(_ context.Context, tx *gorm.DB, _ string,
	query *contenttemplatecontract.TemplateRefQuery) ([]contenttemplatecontract.TemplateReferenceRow, error) {
	s.seenTx = tx
	if s.err != nil {
		return nil, s.err
	}
	if query == nil {
		return nil, nil
	}
	known := make(map[string]bool, len(query.TemplateIDs))
	for _, id := range query.TemplateIDs {
		known[id] = true
	}
	out := make([]contenttemplatecontract.TemplateReferenceRow, 0, len(s.rows))
	for _, row := range s.rows {
		if known[row.TemplateID] {
			out = append(out, row)
		}
	}
	return out, nil
}

// withRefStub 给测试服务装上引用端口，返回 stub 供断言。
func withRefStub(svc *contenttemplateservice.Service, stub *stubTemplateRefs) *stubTemplateRefs {
	svc.SetReferencePort(stub)
	return stub
}

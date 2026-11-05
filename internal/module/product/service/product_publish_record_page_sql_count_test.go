package productservice_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

// TestPublishRecordsReadsOneStatementPerPage 断言发布记录读一页只打一条只读语句：
// 候选、产品级 total、夹页与本页明细都在这一条里。
//
// 计数口径按 probe 记录的真实回调次数（Raw 回调一次就是一条语句），不按 SQL 文本换行拆分 ——
// 整页语句本身是多行的，按行拆会把一条语句算成多条。
func TestPublishRecordsReadsOneStatementPerPage(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()

	support.SeedProjectRow(t, db, projectID, "发布记录分页判据")
	seedListProductsBulk(t, db, projectID, 1, 100)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "发布记录分页判据"}},
	}, stubMasterDataChanges{})
	probe := attachSQLCountProbe(t, db)

	cases := []struct {
		name     string
		keyword  string
		page     int
		wantPage int
		wantRows int
		total    int64
	}{
		{name: "首页", page: 1, wantPage: 1, wantRows: 20, total: 100},
		{name: "越界末页", page: 999, wantPage: 5, wantRows: 20, total: 100},
		{name: "关键词", keyword: "SKU-001", page: 999, wantPage: 1, wantRows: 1, total: 1},
		{name: "空集", keyword: "no-match", page: 999, wantPage: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe.reset()
			got, err := s.QueryPublishRecords(context.Background(), &productdto.ProductPublishRecordPageReq{
				ProjectID: projectID, Keyword: tc.keyword, Page: tc.page,
			})
			if err != nil {
				t.Fatalf("发布记录读模型失败：%v", err)
			}
			if got.Total != tc.total || got.Page != tc.wantPage || len(got.Records) != tc.wantRows {
				t.Fatalf("分页结果错误：total=%d page=%d rows=%d，期望 total=%d page=%d rows=%d（%+v）",
					got.Total, got.Page, len(got.Records), tc.total, tc.wantPage, tc.wantRows, got)
			}
			if got.Limit != productservice.ProductPublishRecordPageSize {
				t.Fatalf("分页事实未归一：%+v", got)
			}
			// 一次请求只一条语句：候选、total、夹页与明细都在这一条里。
			// 计数口径是产品表被读的语句数（按 probe 记录的真实回调，不按 SQL 文本换行拆分）：
			// rls 的 set_config 与事务开启不算 product 读取，不进入这个计数。
			if n := probe.productTableReads(); n != 1 {
				t.Fatalf("整页应只有一条语句读 products（同快照收敛 total 与本页），实际 %d 条：\n%s",
					n, probe.dumpProductTableReads())
			}
			if !strings.Contains(probe.dump(), "presentation_instances") {
				t.Fatalf("整页语句应自身带上发布事实，实际：\n%s", probe.dump())
			}
		})
	}
}

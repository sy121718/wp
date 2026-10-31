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

type publishRecordBatchProbe struct {
	batches [][]string
}

func (p *publishRecordBatchProbe) Langs(_ context.Context, _ string, ids []string) (map[string][]productdto.PublishRecordLangResp, error) {
	p.batches = append(p.batches, append([]string(nil), ids...))
	return map[string][]productdto.PublishRecordLangResp{}, nil
}

func TestPublishRecordsUnfilteredReadsOnlyCurrentPage(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()

	support.SeedProjectRow(t, db, projectID, "发布记录分页判据")
	seedListProductsBulk(t, db, projectID, 1, 100)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "发布记录分页判据"}},
	})
	reader := &publishRecordBatchProbe{}
	s.SetPublishRecordPort(reader)
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
			reader.batches = nil
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
			if !got.Available || got.Limit != productservice.ProductPublishRecordPageSize {
				t.Fatalf("分页事实未归一：%+v", got)
			}
			if tc.wantRows == 0 {
				if len(reader.batches) != 0 {
					t.Fatalf("空页不得调用发布端口（空 id 在本端口意味着全工程）：%+v", reader.batches)
				}
				return
			}
			if len(reader.batches) != 1 {
				t.Fatalf("发布端口应只被调用一次，实际 %d 次：%+v", len(reader.batches), reader.batches)
			}
			if n := len(reader.batches[0]); n != tc.wantRows {
				t.Fatalf("发布端口收到 %d 个商品 id，期望本页 %d 个：%+v", n, tc.wantRows, reader.batches[0])
			}
			stmts := strings.Split(probe.dumpProductTableReads(), "\n")
			if len(stmts) != 2 {
				t.Fatalf("本页应只读 products 两次（count + 本页投影），实际 %d 次：\n%s",
					len(stmts), probe.dumpProductTableReads())
			}
			for _, sql := range stmts {
				lower := strings.ToLower(sql)
				if strings.Contains(lower, "count(") || strings.Contains(sql, "LIMIT") {
					continue
				}
				t.Fatalf("商品投影缺少页容量约束（回到全量读会在内存切页）：%s", sql)
			}
		})
	}
}

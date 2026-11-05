package productservice_test

import (
	"cmp"
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

const (
	listPageBenchReps = 5

	listPageBenchWarmup = 1
)

func TestProductListPageWallClockByProductCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "列表耗时基准")

	probe := attachSQLCountProbe(t, db)
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "列表耗时基准"}},
	}, stubMasterDataChanges{})
	ctx := context.Background()

	t.Logf("环境：%s/%s，Go %s；商品档位 1 / 20 / 100（同一工程累积插入），每档热身 %d 次 + 连测 %d 次",
		runtime.GOOS, runtime.GOARCH, runtime.Version(), listPageBenchWarmup, listPageBenchReps)

	seeded := 0
	for _, n := range []int{1, 20, 100} {
		for seeded < n {
			seeded++
			seedListProduct(t, db, projectID, seeded)
		}
		measureListPage(t, ctx, s, probe, projectID, n, 1)
	}

	measureListPage(t, ctx, s, probe, projectID, 100, 2)
}

func measureListPage(t *testing.T, ctx context.Context, s *productservice.Service, probe *sqlCountProbe,
	projectID string, n, page int) {
	t.Helper()
	newReq := func() *productdto.ListReq {
		return &productdto.ListReq{ProjectID: projectID, Page: page}
	}

	for i := 0; i < listPageBenchWarmup; i++ {
		if _, err := s.QueryProducts(ctx, newReq()); err != nil {
			t.Fatalf("n=%d 第 %d 页：热身调用失败：%v", n, page, err)
		}
	}

	durs := make([]time.Duration, 0, listPageBenchReps)
	sqlCount, lookups, rowCount := 0, 0, 0
	for i := 0; i < listPageBenchReps; i++ {
		probe.reset()
		start := time.Now()
		view, err := s.QueryProducts(ctx, newReq())
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("n=%d 第 %d 页：列表页读模型失败：%v", n, page, err)
		}
		if len(view.Items) == 0 {
			t.Fatalf("n=%d 第 %d 页：成品行 0 条，行装配没走到（计时没有意义）", n, page)
		}
		durs = append(durs, elapsed)
		sqlCount, lookups, rowCount = probe.total(), probe.perProductDetailLookups(), len(view.Items)
	}

	slices.SortFunc(durs, func(a, b time.Duration) int { return cmp.Compare(a, b) })
	t.Logf("n=%3d 第 %d 页：行数=%2d SQL=%2d 逐商品详情=%d | 耗时 min=%-10s 中位=%-10s max=%-10s（%d 次：%s）",
		n, page, rowCount, sqlCount, lookups,
		durs[0], durs[len(durs)/2], durs[len(durs)-1], len(durs), formatDurations(durs))

	if n == 1 || (n == 100 && page == 1) {
		t.Logf("n=%d 第 %d 页的 %d 条语句：\n%s", n, page, sqlCount, probe.dump())
	}
}

func formatDurations(durs []time.Duration) string {
	parts := make([]string, 0, len(durs))
	for _, d := range durs {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, " / ")
}

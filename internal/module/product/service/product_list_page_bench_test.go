package productservice_test

// 商品列表查询耗时记录；SQL次数与逐行查询约束由同包独立用例断言。
// 各档热身后测五次，只报告耗时，不使用受机器负载影响的硬阈值。

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
	// listPageBenchReps 每档连测次数（取奇数，中位数就是正中间那一次，不做插值）。
	listPageBenchReps = 5
	// listPageBenchWarmup 每档计入统计前的热身次数。
	listPageBenchWarmup = 1
)

// TestProductListPageWallClockByProductCount 1 / 20 / 100 三档各测列表页整页的墙钟耗时；
// 100 档再测第 2 页，作为「翻页不带来额外工作量」的参照。
//
// **只记录、不断言耗时**：本用例唯一可能失败的地方是「读模型本身失败 / 装配没走到」
// （那几次计时没有意义，与其把 0ms 记进报告不如当场失败）—— 那不是新的性能判据。
func TestProductListPageWallClockByProductCount(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "列表耗时基准")

	probe := attachSQLCountProbe(t, db)
	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "列表耗时基准"}},
	})
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
	// 第 2 页：OFFSET 分页不该带来逐行成本。SQL 次数由 sql_count 用例判定，这里只看耗时量级。
	measureListPage(t, ctx, s, probe, projectID, 100, 2)
}

// measureListPage 测「n 个商品 × 第 page 页」这一档：热身 → 连测 → 记录耗时与 SQL 次数。
//
// error / 行数两处是**测量的前提校验**（读模型失败时那几次计时毫无意义），
// 不是新判据 —— 真正的判据在 product_list_page_sql_count_test.go 里，本文件不复制一份口径：
// SQL 次数与逐商品详情次数只**记录**，相等性断言留给那边。
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

	// 语句清单只在两档打：最小档（清单本身）与满页档（满簿页形状是否不同）。
	if n == 1 || (n == 100 && page == 1) {
		t.Logf("n=%d 第 %d 页的 %d 条语句：\n%s", n, page, sqlCount, probe.dump())
	}
}

// formatDurations 把一次测量里的全部耗时列出来（中位数之外的抖动信息也在报告里有据可查）。
func formatDurations(durs []time.Duration) string {
	parts := make([]string, 0, len(durs))
	for _, d := range durs {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, " / ")
}

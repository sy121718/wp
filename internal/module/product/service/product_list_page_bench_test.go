package productservice_test

// product_list_page_bench_test.go — 商品列表页的**耗时基准**（dd.txt 第九节 A5「1 / 20 / 100 条商品
// 均有 SQL 次数和耗时基准」与第十一节合并门槛的交付物之一）。
//
// 与本包 product_list_page_sql_count_test.go 的分工是**刻意分开**的两件事：
//   - 那边是**判据**：逐商品详情查询 == 0、三档（1/20/100）SQL 总次数相等 —— 硬断言，
//     往 productRowsFromList 里加一行逐商品 s.Get 必须让它变红（真实输出见
//     docs/audit/product-list-n1-report.md §5）；
//   - 这边是**记录**：墙钟耗时只 t.Logf，**不做任何耗时阈值断言**。
//
// 为什么耗时不断言：阈值断言在负载抖动时随机变红，而变红那一次不指向任何代码缺陷；
// flaky 的门禁会被人关掉，然后连「SQL 次数」这条真判据也一起没人看。耗时结论写进报告、由人复核。
//
// 为什么不用 Go Benchmark（func BenchmarkXxx + -bench）：验收要的是一条可复跑的命令
// （go test -run … -v，输出直接贴进报告），而 -bench 默认不跑；这里也不需要 benchstat 那套
// 统计口径。为什么不新造压测基建：库、种子行、工程桩、SQL 探针全部复用 sql_count 用例的既有件
// （同一测试包 productservice_test），本文件只加「计时 + 记录」。
//
// 测量口径：每档先热身 1 次（连接池建立、prepared statement 编译、PG 计划缓存都不是被测对象），
// 再连测 5 次，记录 min / 中位数 / max；结论用**中位数**，min 只作「这台机器能做到多快」的参考。
// SQL 计数探针全程挂在 db 上（一次调用 17 条语句、每条一次字符串拷贝，量级微秒），三档同一份开销。
// 探针挂的是三条链（Query / Row / Raw）—— 补挂 Row 之前本页被漏计 1 条（ListRowFacts 的 SELECT），
// 所以旧记录里的 SQL=16 是**探针口径**少 1，不是页面少一条语句。

import (
	"context"
	"runtime"
	"sort"
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
// err / LoadFailed / 行数三处是**测量的前提校验**（读模型失败时那几次计时毫无意义），
// 不是新判据 —— 真正的判据在 product_list_page_sql_count_test.go 里，本文件不复制一份口径：
// SQL 次数与逐商品详情次数只**记录**，相等性断言留给那边。
func measureListPage(t *testing.T, ctx context.Context, s *productservice.Service, probe *sqlCountProbe,
	projectID string, n, page int) {
	t.Helper()
	newReq := func() *productdto.ProductListPageReq {
		return &productdto.ProductListPageReq{ProjectID: projectID, Page: page}
	}

	for i := 0; i < listPageBenchWarmup; i++ {
		if _, err := s.ProductListPage(ctx, newReq()); err != nil {
			t.Fatalf("n=%d 第 %d 页：热身调用失败：%v", n, page, err)
		}
	}

	durs := make([]time.Duration, 0, listPageBenchReps)
	sqlCount, lookups, rowCount := 0, 0, 0
	for i := 0; i < listPageBenchReps; i++ {
		probe.reset()
		start := time.Now()
		view, err := s.ProductListPage(ctx, newReq())
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("n=%d 第 %d 页：列表页读模型失败：%v", n, page, err)
		}
		if view.LoadFailed {
			t.Fatalf("n=%d 第 %d 页：列表页被判失败（计时没有意义）", n, page)
		}
		if len(view.Products) == 0 {
			t.Fatalf("n=%d 第 %d 页：成品行 0 条，行装配没走到（计时没有意义）", n, page)
		}
		durs = append(durs, elapsed)
		sqlCount, lookups, rowCount = probe.total(), probe.perProductDetailLookups(), len(view.Products)
	}

	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
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

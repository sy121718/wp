package routers

// wiring_test.go — 装配端口清单与自检的纯逻辑测试（审计 CQ-019）。
//
// 不依赖数据库与完整装配（SetupRoutes 需要两者），只验证清单与自检本身：
// 必需端口缺失时要报出端口名与后果、可选端口缺失不算错、清单漂移会被抓到。
// 这几条正是「缺端口时启动直接失败并指明是哪个端口」的可核对证据。

import (
	"strings"
	"testing"
)

// TestWiringManifestEntriesAreWellFormed 清单自身完整性：端口名唯一、字段非空、
// 类别合法。清单腐烂从这里开始（复制一行忘改端口名，自检就会永远通过）。
func TestWiringManifestEntriesAreWellFormed(t *testing.T) {
	seen := make(map[string]bool, len(wiringManifest))
	for i, e := range wiringManifest {
		if strings.TrimSpace(e.Port) == "" {
			t.Fatalf("第 %d 条端口名为空", i)
		}
		if seen[e.Port] {
			t.Fatalf("端口名重复：%s", e.Port)
		}
		seen[e.Port] = true
		if e.Provider == "" || e.Consumer == "" {
			t.Fatalf("端口 %s 缺提供方 / 消费方", e.Port)
		}
		switch e.Kind {
		case wiringRequiredContract, wiringRequiredPort, wiringOptionalDegraded:
		default:
			t.Fatalf("端口 %s 的类别非法：%q", e.Port, e.Kind)
		}
		// 后果一栏是这张表的价值所在：写不出后果的条目等于没盘点。
		if strings.TrimSpace(e.Consequence) == "" {
			t.Fatalf("端口 %s 没写未注入后果", e.Port)
		}
	}
	if len(wiringManifest) == 0 {
		t.Fatal("端口清单为空")
	}
}

// 端口名常量与清单的对应关系**由编译期保证**：wiringManifest 的每条 Port 写的就是常量本身
// （写错名字编译不过），因此不需要一份手工维护的常量清单去「对账」—— 那份清单此前是第二处真源，
// 新增端口要两处都改，漏改一处只会得到「常量数 63 与清单条目数 65 不一致」这种与真实缺陷
// 无关的失败。真正需要防的是**注入点没进清单**，由 TestEverySetterCallIsInWiringManifest 覆盖。

// TestCheckWiringReportsMissingRequiredPort 缺一个必需端口：报出它的名字与后果。
func TestCheckWiringReportsMissingRequiredPort(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	delete(marks, portProductMasterDataChanges)

	missing, unknown := CheckWiring(marks)
	if len(unknown) != 0 {
		t.Fatalf("不该有清单漂移：%v", unknown)
	}
	if len(missing) != 1 {
		t.Fatalf("应只报 1 个缺失端口，实际 %d 个：%v", len(missing), missing)
	}
	if !strings.Contains(missing[0], portProductMasterDataChanges) {
		t.Fatalf("缺失报告里没有端口名 %s：%s", portProductMasterDataChanges, missing[0])
	}
	if !strings.Contains(missing[0], "审计静默缺失") {
		t.Fatalf("缺失报告里没有未注入后果：%s", missing[0])
	}
}

// TestCheckWiringIgnoresMissingOptionalPorts 只缺可选端口：不算错（可选降级的正当性
// 由清单里的后果一栏承担，不由自检拦）。
func TestCheckWiringIgnoresMissingOptionalPorts(t *testing.T) {
	marks := newWiringMarks()
	optional := 0
	for _, e := range wiringManifest {
		if e.Kind == wiringOptionalDegraded {
			optional++
			continue
		}
		marks.mark(e.Port)
	}
	if optional == 0 {
		t.Fatal("清单里没有可选端口，测试前提不成立")
	}

	missing, unknown := CheckWiring(marks)
	if len(missing) != 0 {
		t.Fatalf("缺可选端口不该报错：%v", missing)
	}
	if len(unknown) != 0 {
		t.Fatalf("不该有清单漂移：%v", unknown)
	}
}

// TestCheckWiringReportsUnknownMarks 清单漂移：标记了未登记的端口要被抓出来
// （否则清单会慢慢与实际装配脱节，越用越不可信）。
func TestCheckWiringReportsUnknownMarks(t *testing.T) {
	marks := newWiringMarks()
	marks.mark(portRuntimeFragCart)
	marks.mark("product.SetSomethingNew")

	missing, unknown := CheckWiring(marks)
	if len(unknown) != 1 || unknown[0] != "product.SetSomethingNew" {
		t.Fatalf("清单漂移应报出 product.SetSomethingNew，实际：%v", unknown)
	}
	if len(missing) == 0 {
		t.Fatal("其余必需端口未标记，应当同时报出缺失")
	}
}

// TestCheckWiringAllMarkedIsClean 全部接入（生产装配正常路径）：无缺失、无漂移。
func TestCheckWiringAllMarkedIsClean(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	missing, unknown := CheckWiring(marks)
	if len(missing) != 0 || len(unknown) != 0 {
		t.Fatalf("全接入时不该有任何报告：missing=%v unknown=%v", missing, unknown)
	}
}

// TestMustAllPortsWiredPanicsWithPortName 缺必需端口时 fail-fast，
// 且报错文案里能直接看到是哪个端口（审计 verification 的第一条）。
func TestMustAllPortsWiredPanicsWithPortName(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	delete(marks, portProjectLocaleRetire)
	delete(marks, portRuntimeFragVisitorReturn)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺必需端口时应当 panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic 值应为字符串，实际 %T", r)
		}
		for _, want := range []string{portProjectLocaleRetire, portRuntimeFragVisitorReturn,
			"禁用语言只记日志不下路由"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic 文案缺少 %q：\n%s", want, msg)
			}
		}
	}()
	mustAllPortsWired(marks)
}

// TestMustAllPortsWiredPassesWhenComplete 全部接入时不 panic（生产与集成测试路径）。
func TestMustAllPortsWiredPassesWhenComplete(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	mustAllPortsWired(marks)
}

// TestRequireWiringPortPanicsWithPortName 提供方未实现契约时的内联断言文案。
func TestRequireWiringPortPanicsWithPortName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("ok=false 时应当 panic")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, portProductAvailability) {
			t.Fatalf("panic 文案应含端口名 %s，实际：%v", portProductAvailability, r)
		}
	}()
	RequireWiringPort(portProductAvailability, false)
}

// TestWiringReportsMissingMediaStaleMarkers 换图失效通知的两条端口缺任一条，
// 自检都要报出**端口名 + 未注入后果**：这两条未接入的表现是「换图后已发布页面 /
// 实例永远停在旧字节」—— 旧变体名继续返回旧字节且带 immutable 长缓存，
// 日志里什么都没有，事后几乎不可能从现象反推到装配。
func TestWiringReportsMissingMediaStaleMarkers(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	delete(marks, portMediaStaleMarkerPage)
	delete(marks, portMediaStaleMarkerPresentation)

	missing, unknown := CheckWiring(marks)
	if len(unknown) != 0 {
		t.Fatalf("不该有清单漂移：%v", unknown)
	}
	if len(missing) != 2 {
		t.Fatalf("应报出 2 条缺失端口，实际 %d 条：%v", len(missing), missing)
	}
	joined := strings.Join(missing, "\n")
	for _, want := range []string{
		portMediaStaleMarkerPage, portMediaStaleMarkerPresentation,
		"换图后已发布页面永不更新", "immutable 长缓存",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺失报告里应含 %q：\n%s", want, joined)
		}
	}
	t.Logf("自检缺失报告：\n%s", joined)

	// 装配末尾的 fail-fast 同样要一次报出这两条（不炸在第一个）。
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺必需端口时 mustAllPortsWired 应当 panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic 值应为字符串，实际 %T", r)
		}
		for _, want := range []string{portMediaStaleMarkerPage, portMediaStaleMarkerPresentation} {
			if !strings.Contains(msg, want) {
				t.Fatalf("启动自检的 panic 文案缺少 %q：\n%s", want, msg)
			}
		}
		t.Logf("启动自检 panic 文案：\n%s", msg)
	}()
	mustAllPortsWired(marks)
}

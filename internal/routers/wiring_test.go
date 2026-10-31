package routers

import (
	"strings"
	"testing"
)

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

		if strings.TrimSpace(e.Consequence) == "" {
			t.Fatalf("端口 %s 没写未注入后果", e.Port)
		}
	}
	if len(wiringManifest) == 0 {
		t.Fatal("端口清单为空")
	}
}

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

func TestCheckWiringReportsUnknownMarks(t *testing.T) {
	marks := newWiringMarks()
	marks.mark(portPublicSearchContent)
	marks.mark("product.SetSomethingNew")

	missing, unknown := CheckWiring(marks)
	if len(unknown) != 1 || unknown[0] != "product.SetSomethingNew" {
		t.Fatalf("清单漂移应报出 product.SetSomethingNew，实际：%v", unknown)
	}
	if len(missing) == 0 {
		t.Fatal("其余必需端口未标记，应当同时报出缺失")
	}
}

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

func TestMustAllPortsWiredPanicsWithPortName(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	delete(marks, portProjectLocaleRetire)
	delete(marks, portPublicSearchProduct)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺必需端口时应当 panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic 值应为字符串，实际 %T", r)
		}
		for _, want := range []string{portProjectLocaleRetire, portPublicSearchProduct,
			"禁用语言只记日志不下路由"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("panic 文案缺少 %q：\n%s", want, msg)
			}
		}
	}()
	mustAllPortsWired(marks)
}

func TestMustAllPortsWiredPassesWhenComplete(t *testing.T) {
	marks := newWiringMarks()
	for _, e := range wiringManifest {
		marks.mark(e.Port)
	}
	mustAllPortsWired(marks)
}

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

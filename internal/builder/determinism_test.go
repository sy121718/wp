// determinism_test.go — 确定性构建不变量（docs/01-overview §3.5）。
//
// 同一 Page Document 重复编译必须产出逐字节相同的 Artifact。
// 该测试抓 map 迭代随机性 / 非确定性排序 / 残留状态导致的输出抖动——
// Go 的 map 遍历顺序随机，任何依赖 map 顺序的拼接都会让该测试红灯。

package builder

import (
	"testing"

	"go_wp/internal/templates"
)

// TestCompileDeterminism 同一文档重复编译 20 次，HTML/CSS 逐字节一致。
func TestCompileDeterminism(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}

	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	first, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	for i := 0; i < 20; i++ {
		got, err := Compile(page, WithComponentSet(set))
		if err != nil {
			t.Fatalf("第 %d 次 Compile: %v", i, err)
		}
		if got.HTML != first.HTML {
			t.Fatalf("第 %d 次编译 HTML 字节不一致\n--- first ---\n%s\n--- 第 %d 次 ---\n%s", i, first.HTML, i, got.HTML)
		}
		if got.CSS != first.CSS {
			t.Fatalf("第 %d 次编译 CSS 字节不一致\n--- first ---\n%s\n--- 第 %d 次 ---\n%s", i, first.CSS, i, got.CSS)
		}
	}
}

// TestCompileDeterminismAcrossSets 不同组件模板 Set 实例编译产物一致
// （防 Set 内共享可变状态污染跨编译结果）。
func TestCompileDeterminismAcrossSets(t *testing.T) {
	setA, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet A: %v", err)
	}
	setB, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet B: %v", err)
	}

	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	gotA, err := Compile(page, WithComponentSet(setA))
	if err != nil {
		t.Fatalf("Compile A: %v", err)
	}
	gotB, err := Compile(page, WithComponentSet(setB))
	if err != nil {
		t.Fatalf("Compile B: %v", err)
	}
	if gotA.HTML != gotB.HTML || gotA.CSS != gotB.CSS {
		t.Fatalf("不同 Set 实例编译产物不一致")
	}
}

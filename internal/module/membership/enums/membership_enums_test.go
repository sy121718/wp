package membershipenums

// membership_enums_test.go — 对外文案白名单（MembershipFacingMessages）与 enums 常量的逐条对账。
//
// 为什么要这条测试：白名单是 handler 归口助手（inbound/http/membership_err.go）与
// 契约出口（service.FacingText）共同的判据 —— **漏写一条**的后果是「该业务错误在页面上
// 显示成一句通用提示」：功能不算坏，但没人会发现，而新增业务文案恰恰是每次改需求都会发生的事；
// **多写一条**没有后果。所以把「本包有多少条对外错误文案常量」与「白名单里有几条」绑在一起：
// 新增一条 Err* 却忘了进白名单，这里直接变红。
//
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。形态照 admin/enums 的同名测试。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// messageExclusions 不进白名单的常量 → 理由（必须写明；这张表本身也要能被审计）。
//
// 成功回执（Msg*）不进白名单：白名单的语义是「未命中就记日志 + 归口」，
// 而 Msg* 永远不会作为错误响应返回（它们在 response.SuccessWithMessage 那一侧）。
var messageExclusions = map[string]string{
	"MsgTierCreated":      "成功回执：永远不作为错误响应返回",
	"MsgTierUpdated":      "成功回执：永远不作为错误响应返回",
	"MsgTierDeleted":      "成功回执：永远不作为错误响应返回",
	"MsgEntitlementSaved": "成功回执：永远不作为错误响应返回",
	"MsgAssignSet":        "成功回执：永远不作为错误响应返回",
	"MsgAssignUnlocked":   "成功回执：永远不作为错误响应返回",
	"ErrInternal":         "归口文案本身：它是未命中白名单时的返回值，不是业务文案",
}

// TestMembershipFacingMessagesCoverAllEnums 本包每个 Err* 常量都必须落在白名单里
// （或在 messageExclusions 里写明理由），且白名单里的值都必须真的是一条常量。
func TestMembershipFacingMessagesCoverAllEnums(t *testing.T) {
	inWhitelist := make(map[string]bool, len(MembershipFacingMessages))
	for _, v := range MembershipFacingMessages {
		inWhitelist[v] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	declared := map[string]string{} // 常量值 → 常量名（用于第二段「白名单里不能有错别字」）
	checked := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", file, perr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Err") && !strings.HasPrefix(name.Name, "Msg") {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("%s 的 %s 取值失败: %v", file, name.Name, uerr)
					}
					checked++
					declared[value] = name.Name
					if inWhitelist[value] {
						continue
					}
					if _, ok := messageExclusions[name.Name]; ok {
						continue
					}
					t.Errorf("%s = %q 既不在 MembershipFacingMessages 里，也没有在 messageExclusions 写明理由", name.Name, value)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何 Err* / Msg* 常量：判据失效（源码结构变了？）")
	}
	// 反向：白名单里的每一条都必须对应一条真实常量（防错别字 —— 写错一个字符的白名单条目
	// 不会命中任何错误，于是那条业务文案永远走归口分支，且没有任何迹象）。
	for _, v := range MembershipFacingMessages {
		if _, ok := declared[v]; !ok {
			t.Errorf("白名单里的 %q 不对应本包任何 Err* / Msg* 常量（拼写错误？）", v)
		}
	}
}

// TestEntitlementKindsMatchConstants 权益类型清单与两个常量一一对应。
//
// EntitlementKinds 是「上界封闭」的清单：新增一种权益要同时改 enums、迁移 462 的 CHECK
// 与 462a 的词条 —— 三处漏一处都会在运行时暴露，这里先钉住第一处。
func TestEntitlementKindsMatchConstants(t *testing.T) {
	if len(EntitlementKinds) != 2 {
		t.Fatalf("EntitlementKinds = %v，本期恰好两种", EntitlementKinds)
	}
	if EntitlementKinds[0] != KindFreeShipping || EntitlementKinds[1] != KindDiscount {
		t.Errorf("EntitlementKinds = %v", EntitlementKinds)
	}
}

// TestWithDetailAndHitFacingMessage 带定位信息的形态能被拼出来、也能被认回来。
//
// 这对函数是「业务文案 + 可定位数据」这条协议的唯一实现：拼错了 → 页面上显示裸 key；
// 认错了 → 可行动的业务提示被归口成通用提示（两者都不报错）。
func TestWithDetailAndHitFacingMessage(t *testing.T) {
	msg := WithDetail(ErrDefaultTierMissing, "3f1a2b4c-0000-0000-0000-000000000000")
	if msg == ErrDefaultTierMissing {
		t.Fatal("带定位信息时应拼上分隔符")
	}
	got, ok := HitFacingMessage(msg)
	if !ok || got != msg {
		t.Fatalf("HitFacingMessage(%q) = %q, %v", msg, got, ok)
	}
	key, detail, ok := SplitFacingDetail(msg)
	if !ok || key != ErrDefaultTierMissing || detail != "3f1a2b4c-0000-0000-0000-000000000000" {
		t.Fatalf("SplitFacingDetail = %q, %q, %v", key, detail, ok)
	}
	// 空定位信息不拼分隔符（否则页面上会多出一个孤零零的冒号）。
	if WithDetail(ErrNotFound, "   ") != ErrNotFound {
		t.Errorf("空定位信息不该拼分隔符")
	}
	// 内部错误原文不在白名单里 —— 它必须走归口分支。
	if _, ok := HitFacingMessage(`pq: relation "membership_tiers" does not exist`); ok {
		t.Errorf("数据库原文不该命中白名单")
	}
	// 半角分隔符也被认（读侧归一用）。
	if _, ok := HitFacingMessage(ErrTierNameTaken + ": 白银会员"); !ok {
		t.Errorf("半角分隔符形态应被识别")
	}
}

package blockhttp

// block_page_handle_test.go — 块管理页「失败反馈」的纯逻辑护栏。
//
// 现象（用户报告）：在 /admin/blocks 点了「新建并编辑」之后，浏览器停在一张只有
// ErrBlockDuplicate 字样的空白页上 —— 没有页面壳、不是中文、也没有回列表的入口。
// 两个成因就在这里守住：
//
//  1. 业务错误必须能被认出来（blockErrKey）：认不出来的会被当成内部故障（通用提示），
//     而同名冲突、被引用、类型非法这些都是用户自己就能改正的业务失败；
//  2. 失败必须能回到列表页（blockListURL）：本页不再有「只输出一行裸文本」的第二条通道。
//
// 断言的是映射结果本身，不是「函数没报错」。

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"

	"gorm.io/gorm"
)

// TestBlockErrKeyBusinessErrors 每一个业务 sentinel 都要映射到自己的词条 key。
//
// 漏登记一个 → 它被当成内部错误 → 用户只看到「系统内部错误，请稍后重试」，
// 而日志里那条 error 与用户看到的提示对不上号。
func TestBlockErrKeyBusinessErrors(t *testing.T) {
	sentinels := []error{
		blockcontract.ErrParamRequired,
		blockcontract.ErrNotFound,
		blockcontract.ErrProjectNotFound,
		blockcontract.ErrProjectRequired,
		blockcontract.ErrNameRequired,
		blockcontract.ErrInvalidDoc,
		blockcontract.ErrInvalidKind,
		blockcontract.ErrInvalidCategory,
		blockcontract.ErrDuplicate,
		blockcontract.ErrInvalidReuseMode,
		blockcontract.ErrBlockInUse,
	}
	for _, err := range sentinels {
		got := blockErrKey(err)
		if got == "" {
			t.Fatalf("业务错误 %v 未被识别为业务错误（会被当成内部故障）", err)
		}
		if got != err.Error() {
			t.Fatalf("业务错误的词条 key 应等于其 Error()（enums 常量即 key），实际 %q vs %q", got, err.Error())
		}
	}
	// 包装后的错误同样要认得出来（service 内部可能用 %w 套一层）。
	if got := blockErrKey(fmt.Errorf("定位块失败: %w", blockcontract.ErrNotFound)); got != blockcontract.ErrNotFound.Error() {
		t.Fatalf("包装后的业务错误应仍被识别，实际 %q", got)
	}
}

// TestBlockErrKeyNonBusinessErrors 基础设施错误一律判空（原文只进日志，不上面）。
func TestBlockErrKeyNonBusinessErrors(t *testing.T) {
	cases := []error{
		nil,
		errors.New("pq: relation \"blocks\" does not exist"),
		gorm.ErrRecordNotFound, // 驱动/ORM 的内部错误，不是本模块的业务语义
	}
	for _, err := range cases {
		if got := blockErrKey(err); got != "" {
			t.Fatalf("非业务错误 %v 应返回空串（回通用提示 + 记日志），实际 %q", err, got)
		}
	}
}

// TestBlockListURL 失败回跳地址：保留工程上下文 + 回带结论文案。
func TestBlockListURL(t *testing.T) {
	if got := blockListURL("", ""); got != "/admin/blocks" {
		t.Fatalf("空参数应为列表页裸地址，实际 %q", got)
	}
	if got := blockListURL("p-1", ""); got != "/admin/blocks?project=p-1" {
		t.Fatalf("只带工程时应只带 project，实际 %q", got)
	}
	// 结论文案是中文，必须走 QueryEscape（否则可能被 query 解析截断/扭曲）。
	want := "/admin/blocks?err=" + url.QueryEscape("同名块已存在") + "&project=p-1"
	if got := blockListURL("p-1", "同名块已存在"); got != want {
		t.Fatalf("带结论文案的回跳地址不对：\n got %q\nwant %q", got, want)
	}
	// 无工程上下文（表单没带 projectId）时也必须回得去，不能拼出 ?err 打头的怪地址。
	if got := blockListURL("  ", " 缺少块 id "); got != "/admin/blocks?err="+url.QueryEscape("缺少块 id") {
		t.Fatalf("无工程时的回跳地址不对，实际 %q", got)
	}
}

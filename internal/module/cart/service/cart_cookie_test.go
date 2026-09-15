package cartservice

// cart_cookie_test.go — 购物车 cookie 编解码的包内测试。
//
// 为什么放在 service 包里而不是 public/test：codec 是**未导出**的（它只该被本模块
// 的用例使用），而这一层恰恰是最需要密集覆盖的地方 —— 签名校验、篡改拒绝、
// 边界裁剪全是纯逻辑，不需要数据库，跑一次不到一秒。
// 跨模块的链路测试在 public/test/cart/feature/（那里用真实 PostgreSQL）。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	cartcontract "go_wp/internal/module/cart/contract"
)

const testSecret = "cart-cookie-test-secret"

// TestCartCookieRejectsExpiredPayload 超过 MaxAge 的签发时间必须判废。
func TestCartCookieRejectsExpiredPayload(t *testing.T) {
	c := newTestCodec()
	expired := cartPayload{
		V: cartCookieVersion,
		T: time.Now().Unix() - int64(cartcontract.CartCookieMaxAgeSeconds) - 1,
		I: []cartPayloadLine{{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 1}},
	}
	raw, err := c.encode(expired)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, ok := c.decode(raw); ok {
		t.Fatal("超过有效期的 cookie 必须判废")
	}
}

func newTestCodec() cookieCodec { return cookieCodec{secret: []byte(testSecret)} }

// TestCartCookieRoundTrip 编码后再解码应当得到同一辆购物车。
func TestCartCookieRoundTrip(t *testing.T) {
	c := newTestCodec()
	payload := cartPayload{V: cartCookieVersion, I: []cartPayloadLine{
		{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 2},
		{VariantID: "22222222-2222-2222-2222-222222222222", Quantity: 1},
	}}
	raw, err := c.encode(payload)
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	got, ok := c.decode(raw)
	if !ok {
		t.Fatal("自己签发的 cookie 应当可以通过校验")
	}
	if len(got.I) != 2 || got.I[0].Quantity != 2 || got.I[1].VariantID != payload.I[1].VariantID {
		t.Fatalf("往返后内容不一致: %+v", got.I)
	}
}

// TestCartCookieRejectsTamperedPayload 改了载荷体（不动签名）必须判废。
func TestCartCookieRejectsTamperedPayload(t *testing.T) {
	c := newTestCodec()
	raw, err := c.encode(cartPayload{V: cartCookieVersion, I: []cartPayloadLine{
		{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 1},
	}})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		t.Fatalf("cookie 值形状不对: %s", raw)
	}
	// 把数量 1 改成 99：攻击者能改的就是这一层（body），签名改不了。
	body, derr := base64.RawURLEncoding.DecodeString(parts[0])
	if derr != nil {
		t.Fatalf("解出载荷失败: %v", derr)
	}
	var p cartPayload
	if jerr := json.Unmarshal(body, &p); jerr != nil {
		t.Fatalf("反序列化失败: %v", jerr)
	}
	p.I[0].Quantity = 99
	edited, merr := json.Marshal(p)
	if merr != nil {
		t.Fatalf("序列化失败: %v", merr)
	}
	forged := base64.RawURLEncoding.EncodeToString(edited) + "." + parts[1]

	if _, ok := c.decode(forged); ok {
		t.Fatal("篡改过载荷的 cookie 必须判废（签名是唯一的防篡改依据）")
	}
}

// TestCartCookieRejectsForeignSecret 用别的密钥签的 cookie 必须判废（换密钥即全体失效）。
func TestCartCookieRejectsForeignSecret(t *testing.T) {
	issuer := cookieCodec{secret: []byte("another-deployment-secret")}
	raw, err := issuer.encode(cartPayload{V: cartCookieVersion, I: []cartPayloadLine{
		{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 1},
	}})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, ok := newTestCodec().decode(raw); ok {
		t.Fatal("别处签发的 cookie 不该被本部署接受")
	}
}

// TestCartCookieRejectsVersionMismatch 版本号不符即判废（不尝试兼容老格式）。
func TestCartCookieRejectsVersionMismatch(t *testing.T) {
	c := newTestCodec()
	raw, err := c.encode(cartPayload{V: cartCookieVersion + 1})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, ok := c.decode(raw); ok {
		t.Fatal("版本不符的 cookie 必须判废，而不是按当前格式硬解")
	}
}

// TestCartCookieGarbageIsInvalid 空值 / 乱值 / 分段数不对一律判废（不 panic）。
func TestCartCookieGarbageIsInvalid(t *testing.T) {
	c := newTestCodec()
	for _, in := range []string{
		"", "   ", "not-a-cookie", "a.b.c", "!!!.###", strings.Repeat("x", maxCookieBytes+10),
	} {
		p, ok := c.decode(in)
		if ok {
			t.Fatalf("非法输入 %q 不该通过校验", in)
		}
		if len(p.I) != 0 {
			t.Fatalf("判废时必须返回空购物车，实际 %+v", p.I)
		}
	}
}

// TestCartCookieNormalizeMergesDuplicateVariants 同一变体出现多行必须合并成一行。
//
// 为什么关键：不合并的话，结算会把同一个变体当成两行分别锁库存，
// 而订单域的快照是按键去重的 —— 两边对不上就是一笔金额被算错的订单。
func TestCartCookieNormalizeMergesDuplicateVariants(t *testing.T) {
	c := newTestCodec()
	raw, err := c.encode(cartPayload{V: cartCookieVersion, I: []cartPayloadLine{
		{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 2},
		{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 3},
	}})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	got, ok := c.decode(raw)
	if !ok {
		t.Fatal("应当通过校验")
	}
	if len(got.I) != 1 || got.I[0].Quantity != 5 {
		t.Fatalf("重复变体应合并为 2+3=5，实际 %+v", got.I)
	}
}

// TestCartCookieNormalizeDropsInvalidLines 数量越界与空 id 的行被丢弃，其余保留。
func TestCartCookieNormalizeDropsInvalidLines(t *testing.T) {
	c := newTestCodec()
	raw, err := c.encode(cartPayload{V: cartCookieVersion, I: []cartPayloadLine{
		{VariantID: "", Quantity: 3},
		{VariantID: "11111111-1111-1111-1111-111111111111", Quantity: 0},
		{VariantID: "22222222-2222-2222-2222-222222222222", Quantity: -5},
		{VariantID: "33333333-3333-3333-3333-333333333333", Quantity: maxLineQuantity + 1},
		{VariantID: "44444444-4444-4444-4444-444444444444", Quantity: 2},
	}})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	got, ok := c.decode(raw)
	if !ok {
		t.Fatal("应当通过校验")
	}
	if len(got.I) != 1 {
		t.Fatalf("只有第 5 行合法，实际 %+v", got.I)
	}
	if got.I[0].VariantID != "44444444-4444-4444-4444-444444444444" {
		t.Fatalf("保留的行不对: %+v", got.I[0])
	}
}

// TestCartCookieRejectsTooManyLines 超出 cookie 容量的行数整体判废（不截断）。
func TestCartCookieRejectsTooManyLines(t *testing.T) {
	c := newTestCodec()
	lines := make([]cartPayloadLine, 0, maxCartLines+1)
	for i := 0; i <= maxCartLines; i++ {
		// id 必须各不相同：重复的变体会被 mergeLines 合并成一行，
		// 那样测的就成了「合并」而不是「行数上限」。
		lines = append(lines, cartPayloadLine{
			VariantID: fmt.Sprintf("11111111-1111-1111-1111-%012d", i),
			Quantity:  1,
		})
	}
	raw, err := c.encode(cartPayload{V: cartCookieVersion, I: lines})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if _, ok := c.decode(raw); ok {
		t.Fatal("超过行数上限的 cookie 必须整体判废：截断会让访客以为东西都在")
	}
}

// TestCartPayloadWithLine 增 / 改 / 删三种操作的结果。
func TestCartPayloadWithLine(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"
	base := cartPayload{V: cartCookieVersion, I: []cartPayloadLine{{VariantID: a, Quantity: 1}}}

	if got := base.withLine(b, 3); len(got.I) != 2 {
		t.Fatalf("新增一行失败: %+v", got.I)
	}
	if got := base.withLine(a, 7); len(got.I) != 1 || got.I[0].Quantity != 7 {
		t.Fatalf("改数量失败: %+v", got.I)
	}
	if got := base.withLine(a, 0); len(got.I) != 0 {
		t.Fatalf("数量 0 应当移除该行: %+v", got.I)
	}
	if got := base.withLine(b, 0); len(got.I) != 1 {
		t.Fatalf("移除不存在的行不该影响其它行: %+v", got.I)
	}
	if got := base.lineOf(a); got != 1 {
		t.Fatalf("lineOf 取数量失败: %d", got)
	}
	if got := base.lineOf(b); got != 0 {
		t.Fatalf("不在车里的变体应当返回 0: %d", got)
	}
}

// TestCartCookieEmptyPayloadIsValid 空购物车的 cookie 是**合法**的（清空后写回的就是它）。
func TestCartCookieEmptyPayloadIsValid(t *testing.T) {
	c := newTestCodec()
	raw, err := c.encode(cartPayload{V: cartCookieVersion})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	if strings.TrimSpace(raw) == "" {
		t.Fatal("空购物车也要有一个可回写的 cookie 值")
	}
	got, ok := c.decode(raw)
	if !ok {
		t.Fatal("空购物车的 cookie 必须能通过校验（否则清空后每次请求都会判废）")
	}
	if len(got.I) != 0 {
		t.Fatalf("空购物车不该有行: %+v", got.I)
	}
}

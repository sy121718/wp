package cartservice

// cart_cookie.go — 购物车 cookie 的编解码与签名。
//
// 为什么购物车状态放在客户端 cookie 而不是服务端会话：
// 访客在登录之前就要能加购 —— 这是电商最基本的一条路径。放进会话就等于
// 「没账号不能逛」，把加购变成一个需要注册才能做的动作。cookie 让购物车在
// 匿名状态下自然存在，登录后由订单域按邮箱把订单关联到账号上。
//
// 为什么签名：
//   1. 我们只信自己签发的购物车。无签名的 cookie 是「服务端信任的客户端数据」，
//      今天它只装变体 id 与数量（价格服务端现算），明天某个人往里加一个
//      「预估运费」字段，伪造就立刻变成漏洞 —— 这条边界现在就钉住，成本极低；
//   2. 匿名写操作的 CSRF 防线是 SameSite=Lax（跨站 POST 不带 cookie）；
//      签名让「构造一个能通过校验的购物车」这件事对本站以外不可行。
//
// 密钥与后台会话同源（auth.session_secret）：签名密钥属于部署，不属于某个身份域。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	cartcontract "go_wp/internal/module/cart/contract"
)

// cookie 的名字与寿命定义在契约包（cartcontract.CartCookieName / CartCookieMaxAgeSeconds）：
// 写它的是访问面的片段处理器，而跨模块只能依赖 contract 与不可变 dto。
const (
	// cartCookieVersion 载荷版本。格式变了就升版本，老 cookie 直接判废 ——
	// 比写兼容代码便宜，也比「解析出半个购物车」安全。
	// v2 起载荷带签发时间 t，decode 时与 CartCookieMaxAgeSeconds 对齐校验。
	cartCookieVersion = 2
	// maxCartLines 购物车最多几行。cookie 有 4KB 上限，且每行都要在结算时
	// 逐个锁库存行；无上限的输入能让一次请求锁住任意多的行。
	maxCartLines = 20
	// maxLineQuantity 单行数量上限（比订单域的 100000 严：前台购物车的输入框
	// 本来就不该接受「买十万件」，那是批发询价，走人工）。
	maxLineQuantity = 999
	// maxCookieBytes cookie 值长度上限（留足余量给同一请求上的其它 cookie）。
	maxCookieBytes = 3500
)

// cartPayload 购物车 cookie 载荷。
type cartPayload struct {
	V int               `json:"v"`
	T int64             `json:"t"` // 签发时间（Unix 秒）；decode 时校验不超过 MaxAge。
	I []cartPayloadLine `json:"i"`
}

// cartPayloadLine 一行：变体 id + 数量。
//
// 只存这两个字段：商品名、价格、图片都不进 cookie —— 它们是**别的模块的事实**，
// 烘进客户端就等于发布一份会过期的副本（改价之后购物车里还是旧价）。
// 每次渲染都从商品域现读，cookie 里只留「买哪个、买几件」。
type cartPayloadLine struct {
	VariantID string `json:"v"`
	Quantity  int    `json:"q"`
}

// cookieCodec 购物车 cookie 编解码器（密钥来自装配层）。
type cookieCodec struct {
	secret []byte
}

// encode 载荷 → 带签名的 cookie 值（base64url(JSON) + "." + hmac 前 16 字节）。
func (c cookieCodec) encode(p cartPayload) (string, error) {
	if p.V == 0 {
		p.V = cartCookieVersion
	}
	if p.T == 0 {
		p.T = time.Now().Unix()
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	return body + "." + c.sign(body), nil
}

// sign 对载荷体签名（HMAC-SHA256 截断到 16 字节：够用且省 cookie 空间）。
func (c cookieCodec) sign(body string) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// decode 解析并校验 cookie 值。
//
// 返回 ok=false 表示这个 cookie 不可信或已损坏（签名不符 / 版本不符 / 结构非法）：
// 调用方据此当空购物车处理**并把它覆盖掉** —— 留着一个解析不了的 cookie，
// 会让下一次请求继续走同一条判废路径。
//
// 注意这里不返回 error：购物车是「方便」，不是「财产」。为一个读不懂的 cookie
// 打断访客正在做的事（加购、结算）是拿用户的耐心去换系统的洁癖。
// 真正的把关在结算的写路径上（库存与价格都由服务端现算）。
func (c cookieCodec) decode(raw string) (p cartPayload, ok bool) {
	empty := cartPayload{V: cartCookieVersion}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return empty, false
	}
	if len(raw) > maxCookieBytes {
		return empty, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return empty, false
	}
	// 常量时间比较：签名校验不该泄漏「前几位对上了」这种信息。
	if !hmac.Equal([]byte(parts[1]), []byte(c.sign(parts[0]))) {
		return empty, false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return empty, false
	}
	var decoded cartPayload
	if err = json.Unmarshal(b, &decoded); err != nil {
		return empty, false
	}
	if decoded.V != cartCookieVersion {
		return empty, false
	}
	if decoded.T <= 0 {
		return empty, false
	}
	now := time.Now().Unix()
	if now < decoded.T || now-decoded.T > int64(cartcontract.CartCookieMaxAgeSeconds) {
		return empty, false
	}
	lines := normalizeLines(decoded.I)
	if len(lines) > maxCartLines {
		// 超出行数上限只可能来自「我们自己写进去的旧格式」，判废而不是截断：
		// 截断会让访客看到一辆不完整的车，而他还以为东西都在。
		return empty, false
	}
	return cartPayload{V: cartCookieVersion, I: lines}, true
}

// normalizeLines 规范化行：丢弃数量越界与 id 为空的行，并合并重复变体。
//
// 合并是必须的：cookie 里的同一个变体出现两行时，结算会把它当成两行分别锁库存，
// 而订单域的快照是按键去重的 —— 两边对不上就是一笔金额算错。
func normalizeLines(in []cartPayloadLine) []cartPayloadLine {
	if len(in) == 0 {
		return nil
	}
	out := make([]cartPayloadLine, 0, len(in))
	seen := make(map[string]int, len(in))
	for _, l := range in {
		id := strings.TrimSpace(l.VariantID)
		if id == "" || l.Quantity <= 0 || l.Quantity > maxLineQuantity {
			continue
		}
		if idx, dup := seen[id]; dup {
			merged := out[idx].Quantity + l.Quantity
			if merged > maxLineQuantity {
				merged = maxLineQuantity
			}
			out[idx].Quantity = merged
			continue
		}
		seen[id] = len(out)
		out = append(out, cartPayloadLine{VariantID: id, Quantity: l.Quantity})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cartLines 载荷 → 供展示与结算用的行列表。
func (p cartPayload) cartLines() []cartPayloadLine {
	return normalizeLines(p.I)
}

// withLine 返回「加入/设置某变体数量」后的新载荷（0 表示移除）。
func (p cartPayload) withLine(variantID string, quantity int) cartPayload {
	lines := p.cartLines()
	out := make([]cartPayloadLine, 0, len(lines)+1)
	replaced := false
	for _, l := range lines {
		if l.VariantID != variantID {
			out = append(out, l)
			continue
		}
		if quantity > 0 {
			out = append(out, cartPayloadLine{VariantID: variantID, Quantity: quantity})
		}
		replaced = true
	}
	if !replaced && quantity > 0 {
		out = append(out, cartPayloadLine{VariantID: variantID, Quantity: quantity})
	}
	return cartPayload{V: cartCookieVersion, T: p.T, I: out}
}

// lineOf 取某变体的当前数量（不在车里为 0）。
func (p cartPayload) lineOf(variantID string) int {
	for _, l := range p.cartLines() {
		if l.VariantID == variantID {
			return l.Quantity
		}
	}
	return 0
}

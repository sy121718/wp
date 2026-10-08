package cartservice

// 购物车里只存「买哪个变体、买几件」（cookie 装不下也不该装别的），名称、规格、
// 价格与可用量**每次现读**。这不是性能取舍而是正确性取舍：把价格烘进 cookie，
// 改价之后访客看到的还是旧价，而结算时按新价算钱 —— 差额会变成一次投诉。

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

// 这是「采集链路」的收口端：track.js 在浏览器里按 Sourcebuster 的覆盖规则写 cookie，
// 下单那一刻由这里把 cookie 解释成订单要存的那份快照，然后冗余进 orders.attribution。
//
// 为什么必须在下单那一刻定格：cookie 会过期、UTM 参数会被随手改、访客下次来的来源
// 可能完全不同。订单要留下的是**下单时看到的那一份**，事后任何变动都不该改写历史订单。
//
// 全部 cookie 都缺席时返回 nil（列落 "{}"）：无 JS、禁用 cookie、后台代客下单都会
// 走到这里，那时的正确结果是「没有归因数据」，而不是一个装满空串的对象 ——
// 后者在分析里会被当成「有归因但都是空」，与「没有归因」是两件事。

// 边界只有一条：**只影响运费，不动商品金额**。商品小计由订单域按商品域真源现算
//（cart 里的价格不作数，见 cart_checkout.go），站点策略与会员权益都碰不到它。
//
// 取值链（顺序固定，任一环节命中「免」即 0）：
//
//	① 站点基础运费      —— projects.settings 的 shippingBaseFee（分，0 = 这个站点不收运费）；
//	② 满额免运费        —— shippingFreeThreshold > 0 且商品小计 ≥ 门槛 → 0；
//	③ 会员免运费        —— free_shipping 权益为真时把剩下的置 0。
//
// **全模块只有 shippingTotalOf 一个函数给运费定价**：把「该收多少运费」的判断散到
// 第二处的那天，两处就会开始分叉（一处改了阈值、另一处没改），而分叉的表现是
// 「购物车显示免运费、订单里收了运费」这种只有客户能发现的不一致。
//
// 两个端口都可缺：
//
//	membershipcontract.Reader   未注入 = 会员权益未开启（运费与接入前逐字一致）；
//	projectcontract.ShippingPolicyReader
//	                            未注入 = 站点没配运费（base 恒 0 ⇒ 收不到钱）。
//
// 为什么运费不从请求里取：结算入参里凡是从表单取的东西都是客户端可伪造的
//（参见 renderCheckout 的取值方式）。运费是收银台上的一笔钱，谁传谁就能免单 ——
// 所以本文件的两个输入（站点规则、商品小计）都由服务端自己读。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go_wp/internal/module/cart/contract"
	"go_wp/internal/module/cart/dto"
	"go_wp/internal/module/cart/enums"
	"go_wp/internal/module/membership/contract"
	"go_wp/internal/module/membership/dto"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// cartCurrency 购物车金额的展示币种。
//
// **币种是标签 / 口径，不是算术**：金额本来就是数值（分），改币种只改标签 —— 按产品口径
// 货币由后台全局限定为**单值**，前台不提供货币选择（用户只能改自己的地区），
// 也不存在「同一商品按币种分别定价」这回事。所以这里读全局默认值不会改变任何金额计算。
//
// 读的是 pkg/i18n 的**进程内缓存值**（装配期载入、tick 与保存后刷新），不在请求路径查库；
// 该 getter 内部已保证非空（未配置时回退代码内常量），这里直接透传 —— 再兜一层是永不
// 触发的分支，只会让读者以为它可能返回空串，且与 order 模块的同名包装各抄一份迟早漂移。
func cartCurrency() string {
	return i18n.GetDefaultCurrency()
}

// availabilityLowStockThreshold 低库存提示阈值（<= 这个数就提示「仅剩 N 件」）。
// 与 productVariantAvailability 片段的阈值同口径：两处对同一批库存说不同的话，
// 是最容易被当成 bug 的那种不一致。
const availabilityLowStockThreshold = 5

// 购物车可用性文案：**词条 key + 中文兜底**成对放进 CartItem。
//
// 取词发生在渲染侧（fragments/cart_view.jet 由 runtimefragment 渲染）：service 层没有
// 请求语言，在这里调 tr 只会拿到默认语言、把界面语言写错。渲染侧改成
// `tr(AvailableKey, AvailableText)`（带参数的再走 i18n.FillTranslate）即完成多语言。
//
// key 尽量复用访客片段已有的 site.fragment.stock.*（库存提示在商品卡与购物车里
// 必须是同一句话）；只有库里没有的两句才新引入。带计数的兜底仍用 %d 是因为
// 存量 site.fragment.stock.low 词条本身就是 `%d` 形态（存量占位符由各自批次迁移），
// 新引入的 site.fragment.stock.insufficient 用 `{count}`。
const (
	cartStockKeyOffShelf  = "site.fragment.stock.off_shelf"
	cartStockTextOffShelf = "商品已下架，请移除"

	cartStockKeyOut  = "site.fragment.stock.out"
	cartStockTextOut = "暂时缺货"

	cartStockKeyInsufficient  = "site.fragment.stock.insufficient"
	cartStockTextInsufficient = "库存仅剩 %d 件"

	cartStockKeyLow  = "site.fragment.stock.low"
	cartStockTextLow = "仅剩 %d 件"

	cartStockKeyIn  = "site.fragment.stock.in"
	cartStockTextIn = "库存充足"

	cartStockKeyCheckout  = "site.fragment.stock.checkout"
	cartStockTextCheckout = "以结算时库存为准"
)

// View 只读查询购物车。
func (s *Service) View(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, errors.New(cartenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, errors.New(cartenums.ErrProjectRequired)
	}
	p, _ := s.codec.decode(req.Cookie)
	// 只读路径不回写 cookie（哪怕它已经损坏）：GET 带副作用会让缓存与并发都变得可疑，
	// 坏 cookie 会在下一次变更操作时被覆盖掉。
	return s.snapshotOf(ctx, projectID, p, "")
}

// Add 加入购物车：同变体累加数量。
func (s *Service) Add(ctx context.Context, req *cartdto.CartAddReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, errors.New(cartenums.ErrInvalidParam)
	}
	projectID, variantID, err := s.validateLine("", req.ProjectID, req.VariantID)
	if err != nil {
		return nil, err
	}
	if req.Quantity <= 0 || req.Quantity > maxLineQuantity {
		return nil, errors.New(cartenums.ErrQuantityInvalid)
	}

	// 商品必须在：加购时就校验，不让无效项攒到结算页才炸 ——
	// 「加了五件，结账时告诉我三件已下架」比「加的时候就拦住」差得多。
	if _, err = s.fetchVariant(ctx, projectID, variantID); err != nil {
		return nil, err
	}

	p, _ := s.codec.decode(req.Cookie)
	current := p.lineOf(variantID)
	next := current + req.Quantity
	if next > maxLineQuantity {
		// 越界一律整体拒绝而不是悄悄截到上限：截断会让访客以为加成功了。
		return nil, errors.New(cartenums.ErrQuantityTooMany)
	}
	if current == 0 && len(p.cartLines()) >= maxCartLines {
		return nil, errors.New(cartenums.ErrCartFull)
	}
	// 库存预检（拿得到真源时才做）：拿不到就放行，最终把关在结算的写路径上。
	if err = s.checkAvailability(ctx, projectID, variantID, next); err != nil {
		return nil, err
	}
	return s.applyChange(ctx, projectID, p.withLine(variantID, next))
}

// SetQuantity 设置某变体数量（0 = 移除）。
func (s *Service) SetQuantity(ctx context.Context, req *cartdto.CartSetQuantityReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, errors.New(cartenums.ErrInvalidParam)
	}
	projectID, variantID, err := s.validateLine("", req.ProjectID, req.VariantID)
	if err != nil {
		return nil, err
	}
	if req.Quantity < 0 || req.Quantity > maxLineQuantity {
		return nil, errors.New(cartenums.ErrQuantityInvalid)
	}
	p, _ := s.codec.decode(req.Cookie)
	if p.lineOf(variantID) == 0 && req.Quantity == 0 {
		// 移除一个本来就不在车里的商品：幂等返回当前车，不是错误。
		return s.snapshotOf(ctx, projectID, p, "")
	}
	if req.Quantity == 0 {
		return s.applyChange(ctx, projectID, p.withLine(variantID, 0))
	}
	if _, err = s.fetchVariant(ctx, projectID, variantID); err != nil {
		return nil, err
	}
	if err = s.checkAvailability(ctx, projectID, variantID, req.Quantity); err != nil {
		return nil, err
	}
	return s.applyChange(ctx, projectID, p.withLine(variantID, req.Quantity))
}

// Clear 清空购物车。
func (s *Service) Clear(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, errors.New(cartenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, errors.New(cartenums.ErrProjectRequired)
	}
	return s.applyChange(ctx, projectID, cartPayload{V: cartCookieVersion})
}

// applyChange 把新载荷签名后组装快照（返回的 Cookie 供 inbound 写回响应）。
func (s *Service) applyChange(ctx context.Context, projectID string, p cartPayload) (res *cartdto.CartSnapshot, err error) {
	// 校验载荷大小：cookie 超限时浏览器会**静默丢掉整个 cookie**，
	// 表现是「加着加着购物车空了」。在写之前就量一遍，超了明说。
	if probe, perr := s.codec.encode(p); perr != nil {
		return nil, perr
	} else if len(probe) > maxCookieBytes {
		return nil, errors.New(cartenums.ErrCartFull)
	}
	cookie, err := s.codec.encode(p)
	if err != nil {
		return nil, err
	}
	return s.snapshotOf(ctx, projectID, p, cookie)
}

// validateLine 校验工程与变体参数。
func (s *Service) validateLine(_ string, projectID, variantID string) (pid, vid string, err error) {
	pid = strings.TrimSpace(projectID)
	if pid == "" {
		return "", "", errors.New(cartenums.ErrProjectRequired)
	}
	vid = strings.TrimSpace(variantID)
	if vid == "" {
		return "", "", errors.New(cartenums.ErrVariantRequired)
	}
	return pid, vid, nil
}

// fetchVariant 取单个变体，并校验它属于本工程且已启用。
func (s *Service) fetchVariant(ctx context.Context, projectID, variantID string) (sn *productcontract.VariantSnapshot, err error) {
	snaps, err := s.product.VariantSnapshots(ctx, []string{variantID}, projectID)
	if err != nil {
		return nil, err
	}
	for _, sn := range snaps {
		if sn == nil || sn.VariantID != variantID {
			continue
		}
		// 跨工程加购是越权而不是「查不到」：结论对访客一样（这件商品买不了），
		// 但对日志与排查不一样，所以两边都归到同一个对外文案上。
		//
		// 端口已按工程作用域过滤（别的工程的变体不会出现在快照里），这里到不了 ——
		// 留着当第二道防线：端口契约被改坏时仍然拦得住。
		if !sn.Enabled || (sn.ProjectID != "" && sn.ProjectID != projectID) {
			return nil, errors.New(cartenums.ErrVariantNotFound)
		}
		return sn, nil
	}
	return nil, errors.New(cartenums.ErrVariantNotFound)
}

// checkAvailability 数量是否超过可用量（可用量未知时不拦）。
func (s *Service) checkAvailability(ctx context.Context, projectID, variantID string, quantity int) error {
	if s.availability == nil {
		return nil
	}
	avail, err := s.availability.VariantAvailabilities(ctx, projectID, []string{variantID})
	if err != nil {
		// 读不到真源不算「库存不足」：把一次抖动说成缺货会让整店在访客眼里下架。
		return nil
	}
	q, known := avail[variantID]
	if !known {
		return nil
	}
	if q < quantity {
		return errors.New(cartenums.ErrOutOfStock)
	}
	return nil
}

// snapshotOf 把载荷组装成可渲染快照（商品事实与可用量都在这里现读）。
func (s *Service) snapshotOf(ctx context.Context, projectID string, p cartPayload, cookie string) (res *cartdto.CartSnapshot, err error) {
	lines := p.cartLines()
	res = &cartdto.CartSnapshot{
		Items:    make([]*cartdto.CartItem, 0, len(lines)),
		Currency: cartCurrency(),
		Cookie:   cookie,
	}
	if len(lines) == 0 {
		res.Empty = true
		return res, nil
	}

	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.VariantID)
	}
	snaps, err := s.product.VariantSnapshots(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snaps))
	for _, sn := range snaps {
		if sn != nil {
			byID[sn.VariantID] = sn
		}
	}

	avail := map[string]int{}
	if s.availability != nil {
		if m, aerr := s.availability.VariantAvailabilities(ctx, projectID, ids); aerr == nil && m != nil {
			avail = m
		}
	}

	for _, l := range lines {
		item := &cartdto.CartItem{
			VariantID:   l.VariantID,
			Quantity:    l.Quantity,
			MaxQuantity: maxLineQuantity,
			Removable:   true,
		}
		sn := byID[l.VariantID]
		if sn == nil || !sn.Enabled || (sn.ProjectID != "" && sn.ProjectID != projectID) {
			// 下架的商品留在车里让访客自己删，但**不计入小计** ——
			// 把下架商品的钱算进总额，会让购物车的数字和结算页永远对不上。
			item.Missing = true
			item.AvailableKey, item.AvailableText = cartStockKeyOffShelf, cartStockTextOffShelf
			item.MaxQuantity = 0
			res.Items = append(res.Items, item)
			continue
		}

		item.ProductID = sn.ProductID
		item.ProductName = sn.ProductName
		item.VariantLabel = sn.VariantLabel
		item.SKU = sn.SKU
		item.UnitPrice = sn.Price
		item.LineTotal = sn.Price * int64(l.Quantity)
		item.UnitPriceLabel = centsLabel(sn.Price)
		item.LineTotalLabel = centsLabel(item.LineTotal)

		if q, known := avail[l.VariantID]; known {
			item.Available = q
			item.AvailableKnown = true
			item.InStock = q > 0
			switch {
			case q <= 0:
				item.AvailableKey, item.AvailableText = cartStockKeyOut, cartStockTextOut
			case q < l.Quantity:
				item.AvailableKey = cartStockKeyInsufficient
				item.AvailableText = fmt.Sprintf(cartStockTextInsufficient, q)
			case q <= availabilityLowStockThreshold:
				item.AvailableKey = cartStockKeyLow
				item.AvailableText = fmt.Sprintf(cartStockTextLow, q)
			default:
				item.AvailableKey, item.AvailableText = cartStockKeyIn, cartStockTextIn
			}
			if q > 0 && q < item.MaxQuantity {
				item.MaxQuantity = q
			}
		} else {
			// 「未知」不是「缺货」：库存端口未接入时页面照样可用，结论交给结算。
			item.AvailableKey, item.AvailableText = cartStockKeyCheckout, cartStockTextCheckout
			item.InStock = true
		}

		res.Items = append(res.Items, item)
		res.ItemCount += l.Quantity
		res.Total += item.LineTotal
	}
	res.LineCount = len(res.Items)
	res.TotalLabel = centsLabel(res.Total)
	res.Empty = res.LineCount == 0
	return res, nil
}

// centsLabel 分 → 展示串（仅用于展示，不参与计算）。
func centsLabel(cents int64) string {
	return fmt.Sprintf("¥%.2f", float64(cents)/100)
}

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

// buildAttribution 组装归因快照（auth：全部为空时返回 nil）。
func buildAttribution(c cartdto.TrackCookies, userAgent string, now time.Time) *ordercontract.Attribution {
	cur := parseTrackKV(c.Current)
	fst := parseTrackKV(c.First)
	sess := parseTrackKV(c.Session)
	vis := parseTrackKV(c.Visitor)
	trail := parseTrail(c.Trail, now)
	if len(cur) == 0 && len(fst) == 0 && len(sess) == 0 && len(trail) == 0 {
		return nil
	}

	attr := &ordercontract.Attribution{
		SourceType: kv(cur, "t"),
		Referrer:   kv(cur, "r"),
		UTM:        utmOf(cur),
		Ad: ordercontract.AdInfo{
			GCLID:   kv(cur, "id_gclid"),
			FBCLID:  kv(cur, "id_fbclid"),
			TTCLID:  kv(cur, "id_ttclid"),
			MSCLKID: kv(cur, "id_msclkid"),
			ClickID: kv(cur, "cid"),
		},
		Session: ordercontract.SessionInfo{
			Entry:           kv(sess, "e"),
			Pages:           atoiSafe(kv(sess, "p")),
			Count:           atoiSafe(kv(vis, "n")),
			StartTime:       unixRFC3339(kv(sess, "st")),
			DurationSeconds: durationFrom(kv(sess, "st"), now),
		},
		Device: ordercontract.DeviceInfo{
			// UA 由服务端从请求头取（客户端自报的 UA 可以随手改，而服务端拿到的是
			// 这次请求真正带过来的那个）；设备类型与屏幕由采集脚本给（只有浏览器知道）。
			Type:      kv(sess, "d"),
			UserAgent: strings.TrimSpace(userAgent),
			Screen:    kv(sess, "sc"),
		},
		First: ordercontract.FirstTouch{
			SourceType: kv(fst, "t"),
			Referrer:   kv(fst, "r"),
			UTM:        utmOf(fst),
			Landing:    kv(fst, "rp"),
			At:         unixRFC3339(kv(fst, "ts")),
		},
		// Landing 是「第一次到站的那一页」；本次会话的入口在 Session.Entry。
		// 多会话场景下两者不同，分开存正是为了不把它们混成一个概念。
		Landing: kv(fst, "rp"),
		Trail:   trail,
	}
	return attr
}

// utmOf 从 kv 取 UTM 家族。
func utmOf(m map[string]string) ordercontract.UTMInfo {
	return ordercontract.UTMInfo{
		Source:   kv(m, "s"),
		Medium:   kv(m, "m"),
		Campaign: kv(m, "c"),
		Content:  kv(m, "n"),
		Term:     kv(m, "k"),
		ID:       kv(m, "i"),
	}
}

// kv 取键值（不存在返回空串 —— 归因字段的空值就是「没有」，不需要区分「空」与「缺」）。
func kv(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[key])
}

// parseTrackKV 解析 track.js 写的 "k=v&k=v" 串（值是逐段 URL 编码的）。
func parseTrackKV(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]string, 8)
	for _, seg := range strings.Split(raw, "&") {
		idx := strings.Index(seg, "=")
		if idx <= 0 {
			continue
		}
		key := seg[:idx]
		val, err := url.QueryUnescape(seg[idx+1:])
		if err != nil {
			// 解不开就留原值：归因是分析数据，不该为一个坏字符丢掉整条记录。
			val = seg[idx+1:]
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseTrail 解析浏览轨迹（JSON 数组，每项 [路径, 标题, unix 秒]）。
//
// 停留秒数由**相邻两条的时间差**推出：最后一条用「下单时刻 - 进入时刻」——
// 访客正好在下单页停留着，那一段停留恰恰是最有分析价值的一段。
func parseTrail(raw string, now time.Time) []ordercontract.TrailPage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return nil
	}
	var rows [][]any
	if jerr := json.Unmarshal([]byte(decoded), &rows); jerr != nil {
		return nil
	}
	type entry struct {
		path  string
		title string
		at    int64
	}
	entries := make([]entry, 0, len(rows))
	for _, r := range rows {
		if len(r) < 3 {
			continue
		}
		p, _ := r[0].(string)
		t, _ := r[1].(string)
		ts, _ := r[2].(float64)
		if strings.TrimSpace(p) == "" {
			continue
		}
		entries = append(entries, entry{path: p, title: t, at: int64(ts)})
	}
	if len(entries) == 0 {
		return nil
	}
	out := make([]ordercontract.TrailPage, 0, len(entries))
	for i, e := range entries {
		var secs int
		if i+1 < len(entries) {
			secs = int(entries[i+1].at - e.at)
		} else {
			secs = int(now.Unix() - e.at)
		}
		if secs < 0 {
			secs = 0
		}
		out = append(out, ordercontract.TrailPage{
			URL:     e.path,
			Title:   e.title,
			At:      unixRFC3339(strconv.FormatInt(e.at, 10)),
			Seconds: secs,
		})
	}
	return out
}

// unixRFC3339 unix 秒 → RFC3339（UTC）。
//
// 用 UTC 而不是本地时区：订单归因会跨时区做报表，存一个「本地时间」等于把
// 服务器时区偷偷写进了历史数据里，而那个时区将来一定会变。
func unixRFC3339(s string) string {
	sec := atoi64Safe(s)
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// durationFrom 从起始 unix 秒算到 now 的秒数。
func durationFrom(start string, now time.Time) int {
	sec := atoi64Safe(start)
	if sec <= 0 {
		return 0
	}
	d := int(now.Unix() - sec)
	if d < 0 {
		return 0
	}
	return d
}

// atoiSafe 宽松解析（解析不出来当 0）。
func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// atoi64Safe 宽松解析（解析不出来当 0）。
func atoi64Safe(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// SetMembershipReader 注入会员身份读取端口（装配期调用；**可缺** = 免运费未开启）。
//
// 收窄到 Reader 而不是 MembershipService：购物车只需要「这个访客免不免运费」，
// 拿不到等级 CRUD、归属写入或重算能力（越权防护靠接口形状，不靠调用方自觉）。
func (s *Service) SetMembershipReader(reader membershipcontract.Reader) {
	if s == nil {
		return
	}
	s.membership = reader
}

// SetShippingPolicyReader 注入站点运费规则读取端口（装配期调用；**可缺** = 站点不收运费）。
//
// 收窄到 ShippingPolicyReader（一条只读方法）：结算只需要「这个工程的运费规则」，
// 拿不到工程 CRUD、站点设置写入或主题能力 —— cart 也**不允许** import project 的
// service/model，这条端口是唯一通道（与 product 的 VariantSnapshotPort 同一形状）。
func (s *Service) SetShippingPolicyReader(reader projectcontract.ShippingPolicyReader) {
	if s == nil {
		return
	}
	s.shippingPolicy = reader
}

// shippingPolicyOf 读该工程的运费规则（**结算写路径上唯一读站点设置的地方**）。
//
// 三种形态：
//   - 端口未注入 / 工程为空 → 零值（不收运费），**不打日志**：那是合法的部署形态
//     （站点本来就没配运费），每次结算记一条 Warn 只会把日志淹掉，反而掩盖真故障；
//   - 读失败（基础设施）→ 零值 + Error 日志；
//   - 读到规则 → 原样返回（合法性由 project 侧归一，见 ShippingPolicyReader 的注释）。
//
// 「读失败按 0」是刻意的失效方向，理由见 shippingTotalOf 的注释。
func (s *Service) shippingPolicyOf(ctx context.Context, projectID string) projectcontract.ShippingPolicy {
	if s.shippingPolicy == nil || strings.TrimSpace(projectID) == "" {
		return projectcontract.ShippingPolicy{}
	}
	policy, err := s.shippingPolicy.ShippingPolicyOf(ctx, projectID)
	if err != nil {
		logger.Scene("cart").With("projectId", projectID).Error(err,
			"读站点运费规则失败，本单按不收运费结算（不拒单）")
		return projectcontract.ShippingPolicy{}
	}
	return policy
}

// cartSubtotalOf 结算路径现算商品小计（分）—— 「满额免运费」门槛判定的输入。
//
// 为什么结算里要算这一次：门槛判的是「这一单买了多少钱」，那个数字只有读商品域真源
// 才知道（cookie 里的价格不作数，见本文件的 cookie 编解码）。口径与订单域建单时逐字一致
// （下架、跨工程、数量非正的行都不计入），所以两边对「多少钱」的看法不会分叉。
//
// **这不是「在别处重算金额」**：运费金额只在 shippingTotalOf 里算一处，这里给的是
// **商品**金额；订单域建单时会再按商品域真源算一遍并落快照 —— 那一份才是权威口径，
// cart 这一份只用来决定「门槛是否已达标」。
//
// 读不到商品事实时返回 0：门槛恒 > 0，于是 0 一定「不达门槛」= 照收基础运费，
// 落在失效方向安全的那一侧（宁愿照收，也不凭空免掉）。而真走到这一步时，订单域
// 建单也会因同一次读取失败而拒绝整单，运费算多少都不改变结局。
func (s *Service) cartSubtotalOf(ctx context.Context, projectID string, lines []cartPayloadLine) int64 {
	if s.product == nil || len(lines) == 0 {
		return 0
	}
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.VariantID)
	}
	snaps, err := s.product.VariantSnapshots(ctx, ids, projectID)
	if err != nil {
		return 0
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snaps))
	for _, sn := range snaps {
		if sn != nil {
			byID[sn.VariantID] = sn
		}
	}
	var subtotal int64
	for _, l := range lines {
		if l.Quantity <= 0 {
			continue
		}
		sn := byID[l.VariantID]
		// 下架 / 跨工程 / 拿不到价格的行不计入小计：与 snapshotOf 对下架商品的处理
		// 同口径 —— 不计入小计的那一行，也不该把客户推过免运费门槛。
		if sn == nil || !sn.Enabled || sn.Price <= 0 {
			continue
		}
		if sn.ProjectID != "" && sn.ProjectID != projectID {
			continue
		}
		subtotal += sn.Price * int64(l.Quantity)
	}
	return subtotal
}

// shippingTotalOf 定出本单该收的运费（分）—— **全模块唯一给运费定价的函数**。
//
// 五种形态都是明确结论：
//   - 站点基础运费 <= 0 → 0（这个站点不收运费，**不读库也不问会员** ——
//     「0 减 0 还是 0」没有结论价值，而结算路径上每一次多余往返都是访客在等）；
//   - 小计已达门槛（门槛 > 0）→ 0（**不再问会员**：已经免了，再解析一次身份
//     只是多一次往返）；
//   - 端口未注入 / 无账号 → 照收基础运费（正常路径，不打日志）；
//   - 有 free_shipping 权益 → 0；
//   - 解析失败 → **照收基础运费** + 一条 Warn。
//
// 失效方向（两条，判据是同一条：宁可按高收，不可凭空免）：
//
//	① 会员身份读不到 → 照收。把「会员服务读不到」解释成「免运费」等于一次数据库抖动
//	   让全站订单免运费（少收钱且不可追溯）；照收的最坏后果是会员少享受一次权益，
//	   客服可补。与折扣侧同一判据（见 order 的 resolveMembershipDiscount）。
//	② 站点运费规则读不到（端口未注入 / 读库失败）→ **按 0（不收运费）**。
//	   这一条方向与①相反，理由是输入不同：① 里 base 是**已知**的，② 里 base 根本不知道。
//	   「不知道」时编造一个金额（无论是 0 还是某个默认值）都没有依据 —— 唯一有依据的
//	   缺省值是 0，因为 shippingBaseFee 的语义就是「0 = 不收运费」，而系统在接入站点策略
//	   之前的行为也正是 0（读不到 = 与接入前逐字一致）。
//	   反过来（读不到就拒单 / 按某个非 0 值收）会把一次装配缺陷或数据库抖动升级成
//	   「全店无法结算」或「凭空多收一笔钱」，两者都比少收一笔运费严重且不可解释。
func (s *Service) shippingTotalOf(
	ctx context.Context,
	projectID string,
	userID *uint64,
	subtotal int64,
	policy projectcontract.ShippingPolicy,
) int64 {
	base := policy.BaseFeeCents
	if base <= 0 {
		return 0
	}
	// ② 满额免运费：门槛 > 0 才算「启用」（0 = 不启用，不能解释成「0 元就免」）。
	if policy.FreeThresholdCents > 0 && subtotal >= policy.FreeThresholdCents {
		return 0
	}
	if s.membership == nil || userID == nil || *userID == 0 {
		return base
	}
	if strings.TrimSpace(projectID) == "" {
		return base
	}
	member, err := s.membership.Resolve(ctx, &membershipdto.ResolveReq{
		ProjectID: projectID,
		UserID:    *userID,
	})
	if err != nil {
		logger.Scene("cart").
			With("projectId", projectID).With("userId", *userID).
			Warn("会员身份解析失败，本单按基准运费结算（不免运费，也不拒单）")
		return base
	}
	if member == nil || !member.FreeShipping {
		return base
	}
	return 0
}

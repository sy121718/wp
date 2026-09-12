package cartservice

// cart_view.go — 购物车的读路径：cookie → 商品事实 → 可渲染的快照。
//
// 购物车里只存「买哪个变体、买几件」（cookie 装不下也不该装别的），名称、规格、
// 价格与可用量**每次现读**。这不是性能取舍而是正确性取舍：把价格烘进 cookie，
// 改价之后访客看到的还是旧价，而结算时按新价算钱 —— 差额会变成一次投诉。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	productcontract "go_wp/internal/module/product/contract"
)

// currencyCNY 购物车与订单当前唯一支持的币种（订单表的 currency 列固定写 CNY）。
const currencyCNY = "CNY"

// availabilityLowStockThreshold 低库存提示阈值（<= 这个数就提示「仅剩 N 件」）。
// 与 productVariantAvailability 片段的阈值同口径：两处对同一批库存说不同的话，
// 是最容易被当成 bug 的那种不一致。
const availabilityLowStockThreshold = 5

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
	if err = s.checkAvailability(ctx, variantID, next); err != nil {
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
	if err = s.checkAvailability(ctx, variantID, req.Quantity); err != nil {
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
	snaps, err := s.product.VariantSnapshots(ctx, []string{variantID})
	if err != nil {
		return nil, err
	}
	for _, sn := range snaps {
		if sn == nil || sn.VariantID != variantID {
			continue
		}
		// 跨工程加购是越权而不是「查不到」：结论对访客一样（这件商品买不了），
		// 但对日志与排查不一样，所以两边都归到同一个对外文案上。
		if !sn.Enabled || (sn.ProjectID != "" && sn.ProjectID != projectID) {
			return nil, errors.New(cartenums.ErrVariantNotFound)
		}
		return sn, nil
	}
	return nil, errors.New(cartenums.ErrVariantNotFound)
}

// checkAvailability 数量是否超过可用量（可用量未知时不拦）。
func (s *Service) checkAvailability(ctx context.Context, variantID string, quantity int) error {
	if s.availability == nil {
		return nil
	}
	avail, err := s.availability.VariantAvailabilities(ctx, []string{variantID})
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
		Currency: currencyCNY,
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
	snaps, err := s.product.VariantSnapshots(ctx, ids)
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
		if m, aerr := s.availability.VariantAvailabilities(ctx, ids); aerr == nil && m != nil {
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
			item.AvailableText = "商品已下架，请移除"
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
				item.AvailableText = "暂时缺货"
			case q < l.Quantity:
				item.AvailableText = fmt.Sprintf("库存仅剩 %d 件", q)
			case q <= availabilityLowStockThreshold:
				item.AvailableText = fmt.Sprintf("仅剩 %d 件", q)
			default:
				item.AvailableText = "库存充足"
			}
			if q > 0 && q < item.MaxQuantity {
				item.MaxQuantity = q
			}
		} else {
			// 「未知」不是「缺货」：库存端口未接入时页面照样可用，结论交给结算。
			item.AvailableText = "以结算时库存为准"
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

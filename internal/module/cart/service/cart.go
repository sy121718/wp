package cartservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	json "go_wp/pkg/jsonx"
	"net/url"
	"strconv"
	"strings"
	"time"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	ordercontract "go_wp/internal/module/order/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

func cartCurrency() string {
	return i18n.GetDefaultCurrency()
}

func (s *Service) View(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, cartenums.Err(cartenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, cartenums.Err(cartenums.ErrProjectRequired)
	}
	p, _ := s.codec.decode(req.Cookie)

	return s.snapshotOf(ctx, projectID, p, "")
}

func (s *Service) Add(ctx context.Context, req *cartdto.CartAddReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, cartenums.Err(cartenums.ErrInvalidParam)
	}
	projectID, variantID, err := s.validateLine("", req.ProjectID, req.VariantID)
	if err != nil {
		return nil, err
	}
	if req.Quantity <= 0 || req.Quantity > maxLineQuantity {
		return nil, cartenums.Err(cartenums.ErrQuantityInvalid)
	}

	if _, err = s.fetchVariant(ctx, projectID, variantID); err != nil {
		return nil, err
	}

	p, _ := s.codec.decode(req.Cookie)
	current := p.lineOf(variantID)
	next := current + req.Quantity
	if next > maxLineQuantity {

		return nil, cartenums.Err(cartenums.ErrQuantityTooMany)
	}
	if current == 0 && len(p.cartLines()) >= maxCartLines {
		return nil, cartenums.Err(cartenums.ErrCartFull)
	}

	if err = s.checkAvailability(ctx, projectID, variantID, next); err != nil {
		return nil, err
	}
	return s.applyChange(ctx, projectID, p.withLine(variantID, next))
}

func (s *Service) SetQuantity(ctx context.Context, req *cartdto.CartSetQuantityReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil || req.Quantity == nil {
		return nil, cartenums.Err(cartenums.ErrInvalidParam)
	}
	quantity := *req.Quantity
	projectID, variantID, err := s.validateLine("", req.ProjectID, req.VariantID)
	if err != nil {
		return nil, err
	}
	if quantity < 0 || quantity > maxLineQuantity {
		return nil, cartenums.Err(cartenums.ErrQuantityInvalid)
	}
	p, _ := s.codec.decode(req.Cookie)
	if p.lineOf(variantID) == 0 && quantity == 0 {

		return s.snapshotOf(ctx, projectID, p, "")
	}
	if quantity == 0 {
		return s.applyChange(ctx, projectID, p.withLine(variantID, 0))
	}
	if _, err = s.fetchVariant(ctx, projectID, variantID); err != nil {
		return nil, err
	}
	if err = s.checkAvailability(ctx, projectID, variantID, quantity); err != nil {
		return nil, err
	}
	return s.applyChange(ctx, projectID, p.withLine(variantID, quantity))
}

func (s *Service) Clear(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error) {
	if req == nil {
		return nil, cartenums.Err(cartenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, cartenums.Err(cartenums.ErrProjectRequired)
	}
	return s.applyChange(ctx, projectID, cartPayload{V: cartCookieVersion})
}

func (s *Service) applyChange(ctx context.Context, projectID string, p cartPayload) (res *cartdto.CartSnapshot, err error) {

	if probe, perr := s.codec.encode(p); perr != nil {
		return nil, perr
	} else if len(probe) > maxCookieBytes {
		return nil, cartenums.Err(cartenums.ErrCartFull)
	}
	cookie, err := s.codec.encode(p)
	if err != nil {
		return nil, err
	}
	return s.snapshotOf(ctx, projectID, p, cookie)
}

func (s *Service) validateLine(_ string, projectID, variantID string) (pid, vid string, err error) {
	pid = strings.TrimSpace(projectID)
	if pid == "" {
		return "", "", cartenums.Err(cartenums.ErrProjectRequired)
	}
	vid = strings.TrimSpace(variantID)
	if vid == "" {
		return "", "", cartenums.Err(cartenums.ErrVariantRequired)
	}
	return pid, vid, nil
}

func (s *Service) fetchVariant(ctx context.Context, projectID, variantID string) (sn *productcontract.VariantSnapshot, err error) {
	snaps, err := s.product.VariantSnapshots(ctx, []string{variantID}, projectID)
	if err != nil {
		return nil, err
	}
	for _, sn := range snaps {
		if sn == nil || sn.VariantID != variantID {
			continue
		}

		if !sn.Enabled || (sn.ProjectID != "" && sn.ProjectID != projectID) {
			return nil, cartenums.Err(cartenums.ErrVariantNotFound)
		}
		return sn, nil
	}
	return nil, cartenums.Err(cartenums.ErrVariantNotFound)
}

func (s *Service) checkAvailability(ctx context.Context, projectID, variantID string, quantity int) error {
	if s.availability == nil {
		return nil
	}
	avail, err := s.availability.VariantAvailabilities(ctx, projectID, []string{variantID})
	if err != nil {

		return nil
	}
	q, known := avail[variantID]
	if !known {
		return nil
	}
	if q < quantity {
		return cartenums.Err(cartenums.ErrOutOfStock)
	}
	return nil
}

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
		}
		sn := byID[l.VariantID]
		if sn == nil || !sn.Enabled || (sn.ProjectID != "" && sn.ProjectID != projectID) {

			item.Missing = true
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

		if q, known := avail[l.VariantID]; known {
			available := q
			item.Available = &available
			if q <= 0 {
				item.MaxQuantity = 0
			} else if q < item.MaxQuantity {
				item.MaxQuantity = q
			}
		}

		res.Items = append(res.Items, item)
		res.ItemCount += l.Quantity
		res.Total += item.LineTotal
	}
	res.LineCount = len(res.Items)
	res.Empty = res.LineCount == 0
	return res, nil
}

const (
	cartCookieVersion = 2

	maxCartLines = 20

	maxLineQuantity = 999

	maxCookieBytes = 3500
)

type cartPayload struct {
	V int               `json:"v"`
	T int64             `json:"t"`
	I []cartPayloadLine `json:"i"`
}

type cartPayloadLine struct {
	VariantID string `json:"v"`
	Quantity  int    `json:"q"`
}

type cookieCodec struct {
	secret []byte
}

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

func (c cookieCodec) sign(body string) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

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

		return empty, false
	}
	return cartPayload{V: cartCookieVersion, I: lines}, true
}

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

func (p cartPayload) cartLines() []cartPayloadLine {
	return normalizeLines(p.I)
}

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

func (p cartPayload) lineOf(variantID string) int {
	for _, l := range p.cartLines() {
		if l.VariantID == variantID {
			return l.Quantity
		}
	}
	return 0
}

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

		Landing: kv(fst, "rp"),
		Trail:   trail,
	}
	return attr
}

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

func kv(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[key])
}

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

			val = seg[idx+1:]
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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

func unixRFC3339(s string) string {
	sec := atoi64Safe(s)
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

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

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func atoi64Safe(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func (s *Service) SetMembershipReader(reader membershipcontract.Reader) {
	if s == nil {
		return
	}
	s.membership = reader
}

func (s *Service) SetShippingPolicyReader(reader projectcontract.ShippingPolicyReader) {
	if s == nil {
		return
	}
	s.shippingPolicy = reader
}

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

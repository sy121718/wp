package analyticsservice

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsmodel "go_wp/internal/module/analytics/model"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
)

const (
	maxPathLen = 512

	maxReferrerLen = 255
)

var langPattern = regexp.MustCompile("^[A-Za-z0-9-]{1,35}$")

func (s *Service) Collect(ctx context.Context, req *analyticsdto.CollectReq) (err error) {
	if req == nil {
		return nil
	}
	e, ok := s.buildEntity(req)
	if !ok {

		return nil
	}
	if ierr := s.m.Insert(ctx, e); ierr != nil {
		logger.Scene("analytics").With("project", e.ProjectID).Error(ierr, "页面浏览写入失败（已静默降级，不影响访客）")
	}
	return nil
}

func (s *Service) buildEntity(req *analyticsdto.CollectReq) (e *analyticsmodel.PageViewEntity, ok bool) {
	projectID := strings.TrimSpace(req.ProjectID)
	if _, perr := uuid.Parse(projectID); perr != nil {

		return nil, false
	}
	path := sanitizeTrackPath(req.Path)
	if path == "" {
		return nil, false
	}
	lang := strings.TrimSpace(req.Lang)
	if !langPattern.MatchString(lang) {
		lang = ""
	}
	return &analyticsmodel.PageViewEntity{
		ProjectID:    projectID,
		Path:         path,
		Lang:         lang,
		SessionID:    s.hashAnon(req.Session),
		VisitorHash:  s.hashAnon(req.Visitor),
		ReferrerHost: normalizeReferrer(req.Referrer),
		UAClass:      uaClass(req.UserAgent),
		IPHash:       s.hashAnon(req.IP),
		ViewedAt:     s.now(),
	}, true
}

func sanitizeTrackPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	if len(path) > maxPathLen {
		path = path[:maxPathLen]
	}
	return path
}

func normalizeReferrer(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	host := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = u.Host
	}

	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	if len(host) > maxReferrerLen {
		host = host[:maxReferrerLen]
	}
	return host
}

var botMarkers = []string{
	"bot", "spider", "crawler", "crawl", "slurp", "curl/", "wget", "python-requests",
	"go-http-client", "headlesschrome", "phantomjs", "monitor", "uptimerobot", "pingdom",
	"facebookexternalhit", "semrush", "ahrefs", "bytespider", "petalbot",
}

func uaClass(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return ""
	}
	lower := strings.ToLower(ua)
	for _, marker := range botMarkers {
		if strings.Contains(lower, marker) {
			return "bot"
		}
	}
	if strings.Contains(lower, "ipad") || strings.Contains(lower, "tablet") ||
		strings.Contains(lower, "playbook") || strings.Contains(lower, "silk") {
		return "tablet"
	}
	if strings.Contains(lower, "mobi") || strings.Contains(lower, "android") ||
		strings.Contains(lower, "iphone") || strings.Contains(lower, "ipod") ||
		strings.Contains(lower, "windows phone") {
		return "mobile"
	}
	return "desktop"
}

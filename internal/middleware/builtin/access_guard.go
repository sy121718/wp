package builtin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
	"go_wp/pkg/auth"
)

const AccessUnlockPath = "/access/unlock"

const (
	accessCookieName = "gw_access"

	accessCookieMaxAgeSeconds = 12 * 3600

	accessCookieVersion = 1

	maxAccessCookiePaths = 20

	maxAccessCookieBytes = 3500

	accessWrongPasswordStatus = http.StatusForbidden
)

var (
	accessProbeMu    sync.RWMutex
	accessLoginProbe func(*gin.Context) bool
)

func SetAccessGuardLoginProbe(fn func(*gin.Context) bool) {
	accessProbeMu.Lock()
	accessLoginProbe = fn
	accessProbeMu.Unlock()
}

func accessVisitorLoggedIn(c *gin.Context) (loggedIn bool) {
	accessProbeMu.RLock()
	fn := accessLoginProbe
	accessProbeMu.RUnlock()
	if fn == nil {
		return false
	}

	defer func() {
		if r := recover(); r != nil {
			loggedIn = false
		}
	}()
	return fn(c)
}

type accessPayload struct {
	V int `json:"v"`

	T int64 `json:"t"`

	F string `json:"f"`

	P []string `json:"p"`
}

func sitePathOf(entry string) string {
	if entry == "" || entry == "index" {
		return "/"
	}
	return "/" + entry
}

func activeEntryDir(entry string) (dir string, ok bool) {
	if entry == "" {
		return "", false
	}
	link := filepath.Join(pipeline.ActiveRoot(), filepath.FromSlash(entry))
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	return filepath.Clean(filepath.Join(filepath.Dir(link), target)), true
}

func isInternalArtifactRequest(clean string) bool {
	base := filepath.Base(filepath.FromSlash(clean))
	return base == pipeline.GuardMetaFileName || base == pipeline.GuardPageFileName
}

func AccessGuardMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		raw, inFace := SiteFaceRel(prefix, c.Request.URL.Path)
		if !inFace {
			c.Next()
			return
		}
		clean, ok := pipeline.CleanSiteRel(raw)
		if !ok {
			c.Next()
			return
		}

		clean = stripContentEncodingSuffix(clean)
		root := pipeline.ActiveRoot()

		entry, isEntry := pipeline.ResolveActiveEntry(root, clean)
		if !isEntry {

			if folded, okFold := resolveEntryFolded(root, clean); okFold {
				entry, isEntry = folded, true
			}
		}
		if !isEntry {

			if isInternalArtifactRequest(clean) {

				c.Header("Cache-Control", "private, no-store")
				c.AbortWithStatus(accessWrongPasswordStatus)
				return
			}
			c.Next()
			return
		}
		dir, hasDir := activeEntryDir(entry)
		if !hasDir {

			c.Next()
			return
		}
		sitePath := sitePathOf(entry)
		guard, gerr := pipeline.ReadGuardMeta(dir)
		if gerr != nil {

			serveGuardDenied(c, nil, dir, sitePath, accessWrongPasswordStatus)
			return
		}
		if guard == nil {
			c.Next()
			return
		}
		if guardAllows(c, guard, entry) {
			c.Next()
			return
		}
		serveGuardDenied(c, guard, dir, sitePath, accessWrongPasswordStatus)
	}
}

func guardAllows(c *gin.Context, guard *pipeline.GuardMeta, entry string) bool {
	switch guard.Type {
	case builder.AccessMembers:
		return accessVisitorLoggedIn(c)
	case builder.AccessPassword:
		p, ok := decodeAccessCookie(c, guard.Fingerprint)
		return ok && p.hasEntry(entry)
	default:
		return false
	}
}

func serveGuardDenied(c *gin.Context, guard *pipeline.GuardMeta, dir, sitePath string, status int) {
	meta := pipeline.GuardMeta{Type: builder.AccessPassword}
	if guard != nil {
		meta = *guard
	}
	c.Header("Cache-Control", "private, no-store")
	if dir != "" {
		if body, ok := pipeline.GuardPageBody(dir); ok {

			c.Data(status, "text/html; charset=utf-8", pipeline.GuardPageWithPath(body, sitePath))
			c.Abort()
			return
		}
	}
	c.Data(status, "text/html; charset=utf-8", pipeline.RenderGuardPage(meta, sitePath, false))
	c.Abort()
}

type accessCodec struct {
	secret []byte
}

func accessCookieCodec() (accessCodec, bool) {
	secret := strings.TrimSpace(auth.SessionSecret())
	if secret == "" {
		return accessCodec{}, false
	}
	return accessCodec{secret: []byte(secret)}, true
}

func (c accessCodec) encode(p accessPayload) (string, error) {
	if p.V == 0 {
		p.V = accessCookieVersion
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

func (c accessCodec) sign(body string) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

func (c accessCodec) decode(raw string) (p accessPayload, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxAccessCookieBytes {
		return accessPayload{}, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return accessPayload{}, false
	}

	if !hmac.Equal([]byte(parts[1]), []byte(c.sign(parts[0]))) {
		return accessPayload{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return accessPayload{}, false
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return accessPayload{}, false
	}
	if p.V != accessCookieVersion {
		return accessPayload{}, false
	}
	if p.T <= 0 || time.Now().Unix()-p.T > accessCookieMaxAgeSeconds {
		return accessPayload{}, false
	}
	if len(p.P) > maxAccessCookiePaths {
		return accessPayload{}, false
	}
	return p, true
}

func (p accessPayload) hasEntry(entry string) bool {
	for _, x := range p.P {
		if x == entry {
			return true
		}
	}
	return false
}

func (p accessPayload) withEntry(entry string) accessPayload {
	if p.hasEntry(entry) {
		return p
	}
	out := append([]string(nil), p.P...)
	out = append(out, entry)
	if len(out) > maxAccessCookiePaths {
		out = out[len(out)-maxAccessCookiePaths:]
	}
	p.P = out
	return p
}

func decodeAccessCookie(c *gin.Context, fingerprint string) (accessPayload, bool) {
	codec, ok := accessCookieCodec()
	if !ok {
		return accessPayload{}, false
	}
	raw, err := c.Cookie(accessCookieName)
	if err != nil || strings.TrimSpace(raw) == "" {
		return accessPayload{}, false
	}
	p, ok := codec.decode(raw)
	if !ok {
		return accessPayload{}, false
	}
	if fingerprint == "" || p.F != fingerprint {
		return accessPayload{}, false
	}
	return p, true
}

func writeAccessCookie(c *gin.Context, p accessPayload) error {
	codec, ok := accessCookieCodec()
	if !ok {
		return errors.New("会话签名密钥未就绪")
	}
	value, err := codec.encode(p)
	if err != nil {
		return err
	}
	secure := c.Request != nil && c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(accessCookieName, value, accessCookieMaxAgeSeconds, "/", "", secure, true)
	return nil
}

func AccessUnlockHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		clean, ok := pipeline.CleanSiteRel(strings.TrimSpace(c.PostForm("path")))
		if !ok {
			serveUnlockFailure(c, nil, "/", false)
			return
		}
		root := pipeline.ActiveRoot()
		entry, isEntry := pipeline.ResolveActiveEntry(root, clean)
		if !isEntry {
			serveUnlockFailure(c, nil, "/", false)
			return
		}

		back := sitePathOf(entry)

		dir, hasDir := activeEntryDir(entry)
		if !hasDir {
			serveUnlockFailure(c, nil, back, false)
			return
		}
		guard, gerr := pipeline.ReadGuardMeta(dir)
		if gerr != nil || guard == nil {

			serveUnlockFailure(c, nil, back, false)
			return
		}
		if guard.Type != builder.AccessPassword {

			serveUnlockFailure(c, guard, back, false)
			return
		}
		password := c.PostForm("password")
		if password == "" || len(password) > builder.MaxAccessPasswordBytes {

			serveUnlockFailure(c, guard, back, true)
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(guard.PasswordHash), []byte(password)) != nil {
			serveUnlockFailure(c, guard, back, true)
			return
		}

		payload := accessPayload{V: accessCookieVersion, T: time.Now().Unix(), F: guard.Fingerprint}
		if existing, ok := decodeAccessCookie(c, guard.Fingerprint); ok {
			payload.P = existing.P
		}
		if err := writeAccessCookie(c, payload.withEntry(entry)); err != nil {
			serveUnlockFailure(c, guard, back, true)
			return
		}
		c.Redirect(http.StatusFound, back)
	}
}

func serveUnlockFailure(c *gin.Context, guard *pipeline.GuardMeta, back string, showError bool) {
	meta := pipeline.GuardMeta{Type: builder.AccessPassword}
	if guard != nil {
		meta = *guard
	}
	c.Header("Cache-Control", "private, no-store")
	c.Data(accessWrongPasswordStatus, "text/html; charset=utf-8",
		pipeline.RenderGuardPage(meta, back, showError))
	c.Abort()
}

var contentEncodingSuffixes = []string{pipeline.PrecompressedSuffix, ".br"}

func stripContentEncodingSuffix(clean string) string {
	for _, suffix := range contentEncodingSuffixes {
		if trimmed, ok := strings.CutSuffix(clean, suffix); ok && trimmed != "" {
			return trimmed
		}
	}
	return clean
}

func resolveEntryFolded(root, clean string) (string, bool) {
	defer func() {}()
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	lowered := strings.ToLower(clean)
	var hit string
	matches := 0
	for _, e := range entries {
		name := strings.ToLower(e.Name())

		if lowered != name && !strings.HasPrefix(lowered, name+"/") {
			continue
		}

		candidate := e.Name() + clean[len(e.Name()):]

		if resolved, ok := pipeline.ResolveActiveEntry(root, candidate); ok {
			hit = resolved
			matches++
		}
	}
	if matches == 1 {
		return hit, true
	}
	return "", false
}

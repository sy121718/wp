package adminui

import (
	"reflect"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	viewpkg "go_wp/pkg/view"
)

const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

func CSRF(c *gin.Context) string {
	return binding.CSRFToken(c)
}

func Prepare(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}

	data = text.Inject(c, data)

	data["csrf_token"] = CSRF(c)

	set := map[string]bool{}
	if v, ok := c.Get(adminctx.PermSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set

	buttons := map[string]bool{}
	if v, ok := c.Get(adminctx.ButtonsKey); ok {
		if m, ok := v.(map[string]bool); ok {
			buttons = m
		}
	}
	data["Buttons"] = buttons

	navGroups := BuildNav(navTreeOf(c), c.Request.URL.Path, translateFor(c))
	data["NavGroups"] = navGroups

	open := sidebarOpen(c)
	pinned := sidebarPinned(c)

	if open && !pinned && !hasActiveDirGroup(navGroups) {
		open = false
	}
	data["SidebarOpen"] = open
	data["SidebarPinned"] = pinned

	data["HasSubnav"] = hasActiveDirGroup(navGroups)

	return data
}

func PrepareView(c *gin.Context, view any) gin.H {
	data := Prepare(c, ViewMap(view))
	injectPageList(data, view)
	return data
}

func injectPageList(data gin.H, view any) {
	page, ok := viewpkg.PageListOf(view)
	if !ok {
		return
	}
	if _, exists := data[viewpkg.PaginationKey]; exists {
		return
	}
	data[viewpkg.PaginationKey] = page
}

func ViewMap(view any) gin.H {
	switch v := view.(type) {
	case nil:
		return gin.H{}
	case gin.H:
		return v
	case map[string]any:
		out := make(gin.H, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	}

	rv := reflect.ValueOf(view)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return gin.H{}
		}
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		out := make(gin.H, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = iter.Value().Interface()
		}
		return out
	}

	if rv.Kind() != reflect.Struct {
		panic("adminui.ViewMap: 视图必须是结构体、结构体指针或 gin.H/map[string]any，" +
			"收到 " + rv.Kind().String() + "（视图装配属于 dto，不要在控制器里手工拼 map）")
	}

	rt := rv.Type()
	out := make(gin.H, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)

		if field.Anonymous {
			ev := rv.Field(i)
			for ev.Kind() == reflect.Pointer {
				if ev.IsNil() {
					ev = reflect.Value{}
					break
				}
				ev = ev.Elem()
			}

			if ev.IsValid() && ev.Kind() == reflect.Struct {
				for k, v := range viewMapOfStruct(ev) {

					if _, exists := out[k]; !exists {
						out[k] = v
					}
				}
				continue
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		name := field.Name
		if tag := field.Tag.Get("view"); tag != "" {
			name = tag
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}

func PrepareBare(c *gin.Context, data gin.H) gin.H {
	return text.Inject(c, data)
}

func translateFor(c *gin.Context) func(key, fallback string) string {
	return text.TranslateFunc(c)
}

func navTreeOf(c *gin.Context) []admindto.MenuTreeNode {
	if v, ok := c.Get(adminctx.NavTreeKey); ok {
		if tree, ok := v.([]admindto.MenuTreeNode); ok {
			return tree
		}
	}
	return nil
}

func requestURIOf(c *gin.Context) string {
	return text.RequestURI(c)
}

func hasActiveDirGroup(groups []NavGroup) bool {
	for _, g := range groups {
		if g.Active && len(g.Nodes) > 0 {
			return true
		}
	}
	return false
}

func sidebarOpen(c *gin.Context) bool {
	v, err := c.Cookie(sidebarCookieOpen)
	if err != nil || v == "" {
		return true
	}
	return v != "0"
}

func sidebarPinned(c *gin.Context) bool {
	v, _ := c.Cookie(sidebarCookiePinned)
	return v == "1"
}

func viewMapOfStruct(rv reflect.Value) gin.H {
	rt := rv.Type()
	out := make(gin.H, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fv := rv.Field(i)
		if field.Anonymous {
			for fv.Kind() == reflect.Pointer {
				if fv.IsNil() {
					fv = reflect.Value{}
					break
				}
				fv = fv.Elem()
			}
			if fv.IsValid() && fv.Kind() == reflect.Struct {
				for k, v := range viewMapOfStruct(fv) {
					if _, exists := out[k]; !exists {
						out[k] = v
					}
				}
			}
			continue
		}
		if !field.IsExported() {
			continue
		}
		name := field.Name
		if tag := field.Tag.Get("view"); tag != "" {
			name = tag
		}

		if !fv.IsValid() {
			continue
		}
		out[name] = fv.Interface()
	}
	return out
}

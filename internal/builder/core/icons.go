// Package core — 共享内置图标集（内联 SVG，fill/stroke 走 currentColor）。
// 组件（list/infobox 等）统一引用，避免各组件重复定义；
// 前端检查器的图标选择器维护同款同名表（icons.js 由本表同步）。
//
// 图标数据经 go:embed 打包（icons/*.svg），构建期零 IO、确定性、可整包替换
// 现成图标库（替换 SVG 文件即可），不再以 Go map 字符串字面量内联。
package core

import (
	"embed"
	"sort"
	"strings"
)

// iconsFS 图标资源 embed.FS（根为 core 包目录）。
//
//go:embed icons/*.svg
var iconsFS embed.FS

// builtinIconSet 内置图标集：viewBox 24，尺寸由外层 CSS（1em）控制。
// 从 icons/*.svg 加载，键 = 文件名（去 .svg 后缀）。
var builtinIconSet = loadBuiltinIcons()

// loadBuiltinIcons 从 embed.FS 加载全部内置图标（进程启动时执行一次）。
func loadBuiltinIcons() map[string]string {
	set := make(map[string]string)
	entries, err := iconsFS.ReadDir("icons")
	if err != nil {
		return set
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		data, err := iconsFS.ReadFile("icons/" + e.Name())
		if err != nil {
			continue
		}
		// TrimSpace：去除 SVG 文件尾部换行，保证与内联字面量字节一致（确定性构建）。
		set[strings.TrimSuffix(e.Name(), ".svg")] = strings.TrimSpace(string(data))
	}
	return set
}

// IconSVG 返回内置图标的内联 SVG；未知名称返回 ok=false。
func IconSVG(name string) (string, bool) {
	svg, ok := builtinIconSet[name]
	return svg, ok
}

// IconNames 返回全部内置图标名（字典序）。
func IconNames() []string {
	names := make([]string, 0, len(builtinIconSet))
	for name := range builtinIconSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IconMeta 图标元数据（分类 + 中文标签 + 搜索关键词 + 风格）。
type IconMeta struct {
	Category string   // 分类（见 IconCategories，共 11 类）
	Label    string   // 中文标签
	Keywords []string // 搜索关键词（英文名 + 中文同义）
	Style    string   // 风格："outlined"（描边）或 "filled"（实心，-fill 后缀）
}

// iconCatalog 图标目录：name → 元数据（与 icons/*.svg 一一对应）。
// 分类与前端 icons.js 的 categories 保持同步。
var iconCatalog = map[string]IconMeta{
	"arrow":           {Category: "arrows", Label: "箭头", Keywords: []string{"arrow", "箭头", "方向", "向右"}},
	"refresh":         {Category: "arrows", Label: "循环", Keywords: []string{"refresh", "刷新", "循环"}},
	"arrow-left":      {Category: "arrows", Label: "左箭头", Keywords: []string{"arrow-left", "左箭头", "向左"}},
	"arrow-up":        {Category: "arrows", Label: "上箭头", Keywords: []string{"arrow-up", "上箭头", "向上"}},
	"arrow-down":      {Category: "arrows", Label: "下箭头", Keywords: []string{"arrow-down", "下箭头", "向下"}},
	"arrow-up-right":  {Category: "arrows", Label: "右上箭头", Keywords: []string{"arrow-up-right", "右上箭头", "外链"}},
	"chevron-down":    {Category: "arrows", Label: "下折叠", Keywords: []string{"chevron-down", "折叠", "展开"}},
	"chevron-left":    {Category: "arrows", Label: "左折叠", Keywords: []string{"chevron-left", "折叠", "后退"}},
	"chevron-right":   {Category: "arrows", Label: "右折叠", Keywords: []string{"chevron-right", "折叠", "前进"}},
	"chevron-up":      {Category: "arrows", Label: "上折叠", Keywords: []string{"chevron-up", "折叠", "收起"}},
	"refresh-cw":      {Category: "arrows", Label: "刷新", Keywords: []string{"refresh-cw", "刷新", "同步"}},
	"maximize":        {Category: "arrows", Label: "最大化", Keywords: []string{"maximize", "最大化", "放大"}},
	"minimize":        {Category: "arrows", Label: "最小化", Keywords: []string{"minimize", "最小化", "缩小"}},
	"check":           {Category: "basic", Label: "对勾", Keywords: []string{"check", "对勾", "完成", "勾选"}},
	"cross":           {Category: "basic", Label: "叉形", Keywords: []string{"cross", "close", "关闭", "叉", "取消"}},
	"clock":           {Category: "basic", Label: "时钟", Keywords: []string{"clock", "时间", "时钟"}},
	"user":            {Category: "basic", Label: "用户", Keywords: []string{"user", "用户", "账户", "个人"}},
	"plus":            {Category: "basic", Label: "加号", Keywords: []string{"plus", "加号", "添加", "新增"}},
	"minus":           {Category: "basic", Label: "减号", Keywords: []string{"minus", "减号", "删除", "减少"}},
	"search":          {Category: "basic", Label: "搜索", Keywords: []string{"search", "搜索", "查找", "放大镜"}},
	"info":            {Category: "basic", Label: "信息", Keywords: []string{"info", "信息", "说明", "提示"}},
	"alert-circle":    {Category: "basic", Label: "警告圆圈", Keywords: []string{"alert-circle", "警告", "提示"}},
	"alert-triangle":  {Category: "basic", Label: "警告三角", Keywords: []string{"alert-triangle", "警告", "注意", "危险"}},
	"help-circle":     {Category: "basic", Label: "帮助圆圈", Keywords: []string{"help-circle", "帮助", "问号", "疑问"}},
	"check-circle":    {Category: "basic", Label: "对勾圆圈", Keywords: []string{"check-circle", "成功", "完成", "通过"}},
	"home":            {Category: "basic", Label: "首页", Keywords: []string{"home", "首页", "主页", "房子"}},
	"settings":        {Category: "basic", Label: "设置", Keywords: []string{"settings", "设置", "齿轮", "配置"}},
	"more-horizontal": {Category: "basic", Label: "更多横", Keywords: []string{"more-horizontal", "更多", "省略号"}},
	"bookmark":        {Category: "basic", Label: "书签", Keywords: []string{"bookmark", "书签", "收藏"}},
	"calendar":        {Category: "basic", Label: "日历", Keywords: []string{"calendar", "日历", "日期", "日程"}},
	"bolt":            {Category: "commerce", Label: "闪电", Keywords: []string{"bolt", "zap", "闪电", "能量"}},
	"gift":            {Category: "commerce", Label: "礼物", Keywords: []string{"gift", "礼物", "优惠"}},
	"truck":           {Category: "commerce", Label: "卡车", Keywords: []string{"truck", "物流", "配送", "卡车"}},
	"shopping-cart":   {Category: "commerce", Label: "购物车", Keywords: []string{"shopping-cart", "购物车", "购买"}},
	"shopping-bag":    {Category: "commerce", Label: "购物袋", Keywords: []string{"shopping-bag", "购物袋", "购买"}},
	"credit-card":     {Category: "commerce", Label: "信用卡", Keywords: []string{"credit-card", "信用卡", "支付", "付款"}},
	"dollar-sign":     {Category: "commerce", Label: "美元", Keywords: []string{"dollar-sign", "美元", "钱", "货币", "价格"}},
	"tag":             {Category: "commerce", Label: "标签", Keywords: []string{"tag", "标签", "价格"}},
	"bar-chart":       {Category: "commerce", Label: "柱状图", Keywords: []string{"bar-chart", "柱状图", "图表", "数据"}},
	"pie-chart":       {Category: "commerce", Label: "饼图", Keywords: []string{"pie-chart", "饼图", "图表", "数据"}},
	"mail":            {Category: "communication", Label: "邮件", Keywords: []string{"mail", "email", "邮件", "邮箱"}},
	"phone":           {Category: "communication", Label: "电话", Keywords: []string{"phone", "电话", "联系"}},
	"message-circle":  {Category: "communication", Label: "消息圆圈", Keywords: []string{"message-circle", "消息", "评论", "聊天"}},
	"send":            {Category: "communication", Label: "发送", Keywords: []string{"send", "发送", "分享"}},
	"share-2":         {Category: "communication", Label: "分享", Keywords: []string{"share-2", "share", "分享", "转发"}},
	"link":            {Category: "communication", Label: "链接", Keywords: []string{"link", "链接", "连接"}},
	"at-sign":         {Category: "communication", Label: "@符号", Keywords: []string{"at-sign", "邮箱", "艾特", "提及"}},
	"inbox":           {Category: "communication", Label: "收件箱", Keywords: []string{"inbox", "收件箱", "邮件"}},
	"map-pin":         {Category: "navigation", Label: "定位", Keywords: []string{"map-pin", "定位", "地址", "位置"}},
	"navigation":      {Category: "navigation", Label: "导航", Keywords: []string{"navigation", "导航", "路线"}},
	"compass":         {Category: "navigation", Label: "指南针", Keywords: []string{"compass", "指南针", "方向"}},
	"globe":           {Category: "navigation", Label: "地球", Keywords: []string{"globe", "地球", "全球", "国际化"}},
	"map":             {Category: "navigation", Label: "地图", Keywords: []string{"map", "地图", "位置"}},
	"target":          {Category: "navigation", Label: "目标", Keywords: []string{"target", "目标", "靶心"}},
	"external-link":   {Category: "navigation", Label: "外链", Keywords: []string{"external-link", "外链", "新窗口"}},
	"crosshair":       {Category: "navigation", Label: "准星", Keywords: []string{"crosshair", "准星", "瞄准", "定位"}},
	"shield":          {Category: "security", Label: "盾牌", Keywords: []string{"shield", "盾牌", "安全", "防护"}},
	"lock":            {Category: "security", Label: "锁", Keywords: []string{"lock", "锁", "锁定", "加密"}},
	"unlock":          {Category: "security", Label: "解锁", Keywords: []string{"unlock", "解锁", "开放"}},
	"eye":             {Category: "security", Label: "眼睛", Keywords: []string{"eye", "眼睛", "查看", "可见"}},
	"eye-off":         {Category: "security", Label: "闭眼", Keywords: []string{"eye-off", "闭眼", "隐藏", "不可见"}},
	"shield-off":      {Category: "security", Label: "盾牌关闭", Keywords: []string{"shield-off", "盾牌", "不安全"}},
	"key":             {Category: "security", Label: "钥匙", Keywords: []string{"key", "钥匙", "密钥", "密码"}},
	"heart":           {Category: "social", Label: "爱心", Keywords: []string{"heart", "爱心", "喜欢", "收藏"}},
	"star":            {Category: "social", Label: "星形", Keywords: []string{"star", "星形", "评分", "收藏"}},
	"thumbs-up":       {Category: "social", Label: "点赞", Keywords: []string{"thumbs-up", "点赞", "喜欢", "赞"}},
	"thumbs-down":     {Category: "social", Label: "点踩", Keywords: []string{"thumbs-down", "点踩", "踩", "不喜欢"}},
	"users":           {Category: "social", Label: "用户组", Keywords: []string{"users", "用户组", "团队", "多人"}},
	"user-plus":       {Category: "social", Label: "添加用户", Keywords: []string{"user-plus", "添加用户", "邀请"}},
	"user-check":      {Category: "social", Label: "用户勾选", Keywords: []string{"user-check", "用户勾选", "成员"}},
	"github":          {Category: "social", Label: "GitHub", Keywords: []string{"github", "代码托管"}},
	"facebook":        {Category: "social", Label: "Facebook", Keywords: []string{"facebook", "脸书"}},
	"youtube":         {Category: "social", Label: "YouTube", Keywords: []string{"youtube", "视频平台"}},
	"image":           {Category: "media", Label: "图片", Keywords: []string{"image", "图片", "图像", "照片"}},
	"video":           {Category: "media", Label: "视频", Keywords: []string{"video", "视频", "影像"}},
	"music":           {Category: "media", Label: "音乐", Keywords: []string{"music", "音乐", "音频"}},
	"camera":          {Category: "media", Label: "相机", Keywords: []string{"camera", "相机", "拍照", "摄影"}},
	"play":            {Category: "media", Label: "播放", Keywords: []string{"play", "播放", "开始"}},
	"play-circle":     {Category: "media", Label: "播放圆圈", Keywords: []string{"play-circle", "播放", "开始"}},
	"pause":           {Category: "media", Label: "暂停", Keywords: []string{"pause", "暂停"}},
	"volume-2":        {Category: "media", Label: "音量", Keywords: []string{"volume-2", "音量", "声音"}},
	"volume-x":        {Category: "media", Label: "静音", Keywords: []string{"volume-x", "静音", "无声"}},
	"mic":             {Category: "media", Label: "麦克风", Keywords: []string{"mic", "麦克风", "录音"}},
	"headphones":      {Category: "media", Label: "耳机", Keywords: []string{"headphones", "耳机", "音频"}},
	"monitor":         {Category: "media", Label: "显示器", Keywords: []string{"monitor", "显示器", "屏幕", "电脑"}},
	"smartphone":      {Category: "media", Label: "手机", Keywords: []string{"smartphone", "手机", "移动端"}},
	"edit-3":          {Category: "editor", Label: "编辑", Keywords: []string{"edit-3", "edit", "编辑", "修改", "铅笔"}},
	"trash-2":         {Category: "editor", Label: "删除", Keywords: []string{"trash-2", "trash", "删除", "垃圾桶"}},
	"copy":            {Category: "editor", Label: "复制", Keywords: []string{"copy", "复制", "拷贝"}},
	"clipboard":       {Category: "editor", Label: "剪贴板", Keywords: []string{"clipboard", "剪贴板", "粘贴"}},
	"save":            {Category: "editor", Label: "保存", Keywords: []string{"save", "保存", "存储"}},
	"type":            {Category: "editor", Label: "文字", Keywords: []string{"type", "文字", "字体", "文本"}},
	"bold":            {Category: "editor", Label: "加粗", Keywords: []string{"bold", "加粗", "粗体"}},
	"italic":          {Category: "editor", Label: "斜体", Keywords: []string{"italic", "斜体"}},
	"align-left":      {Category: "editor", Label: "左对齐", Keywords: []string{"align-left", "左对齐", "对齐"}},
	"code":            {Category: "editor", Label: "代码", Keywords: []string{"code", "代码", "编程"}},
	"filter":          {Category: "editor", Label: "过滤", Keywords: []string{"filter", "过滤", "筛选"}},
	"grid":            {Category: "editor", Label: "网格", Keywords: []string{"grid", "网格", "宫格", "布局"}},
	"list":            {Category: "editor", Label: "列表", Keywords: []string{"list", "列表", "清单"}},
	"menu":            {Category: "editor", Label: "菜单", Keywords: []string{"menu", "菜单", "汉堡", "导航"}},
	"sun":             {Category: "weather", Label: "太阳", Keywords: []string{"sun", "太阳", "晴天"}},
	"moon":            {Category: "weather", Label: "月亮", Keywords: []string{"moon", "月亮", "夜间"}},
	"cloud":           {Category: "weather", Label: "云", Keywords: []string{"cloud", "云", "多云"}},
	"cloud-rain":      {Category: "weather", Label: "雨", Keywords: []string{"cloud-rain", "雨", "下雨"}},
	"cloud-snow":      {Category: "weather", Label: "雪", Keywords: []string{"cloud-snow", "雪", "下雪"}},
	"cloud-lightning": {Category: "weather", Label: "雷雨", Keywords: []string{"cloud-lightning", "雷雨", "闪电"}},
	"umbrella":        {Category: "weather", Label: "伞", Keywords: []string{"umbrella", "伞", "雨伞"}},
	"droplet":         {Category: "weather", Label: "水滴", Keywords: []string{"droplet", "水滴", "液体"}},
	"file":            {Category: "files", Label: "文件", Keywords: []string{"file", "文件", "文档"}},
	"file-text":       {Category: "files", Label: "文本文件", Keywords: []string{"file-text", "文本文件", "文档"}},
	"file-plus":       {Category: "files", Label: "新建文件", Keywords: []string{"file-plus", "新建文件", "添加文件"}},
	"folder":          {Category: "files", Label: "文件夹", Keywords: []string{"folder", "文件夹", "目录"}},
	"folder-plus":     {Category: "files", Label: "新建文件夹", Keywords: []string{"folder-plus", "新建文件夹", "添加目录"}},
	"download":        {Category: "files", Label: "下载", Keywords: []string{"download", "下载"}},
	"upload":          {Category: "files", Label: "上传", Keywords: []string{"upload", "上传"}},
	"archive":         {Category: "files", Label: "归档", Keywords: []string{"archive", "归档", "压缩包"}},
	"database":        {Category: "files", Label: "数据库", Keywords: []string{"database", "数据库", "存储"}},
}

// init 派生实心图标（-fill 后缀）元数据并统一填充风格。
// 实心图标从对应描边图标派生 Category/Label/Keywords（分类保持不变），追加「实心/fill」关键词；
// Style 按后缀填充：-fill → filled，其余 → outlined。
func init() {
	for name := range builtinIconSet {
		if !strings.HasSuffix(name, "-fill") {
			continue
		}
		if _, ok := iconCatalog[name]; ok {
			continue
		}
		base := strings.TrimSuffix(name, "-fill")
		if bm, ok := iconCatalog[base]; ok {
			kw := append([]string{}, bm.Keywords...)
			kw = append(kw, "实心", "fill")
			iconCatalog[name] = IconMeta{Category: bm.Category, Label: bm.Label, Keywords: kw}
		}
	}
	for name, meta := range iconCatalog {
		if strings.HasSuffix(name, "-fill") {
			meta.Style = "filled"
		} else {
			meta.Style = "outlined"
		}
		iconCatalog[name] = meta
	}
}

// IconCategories 返回全部分类（按 iconCatalog 定义顺序去重）。
func IconCategories() []string {
	seen := map[string]bool{}
	order := []string{"arrows", "basic", "commerce", "communication", "navigation", "security", "social", "media", "editor", "weather", "files"}
	out := make([]string, 0, len(order))
	for _, c := range order {
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// IconCategory 返回图标分类；未知图标返回 ok=false。
func IconCategory(name string) (string, bool) {
	meta, ok := iconCatalog[name]
	return meta.Category, ok
}

// IconLabel 返回图标中文标签；未标注返回原名称。
func IconLabel(name string) string {
	if meta, ok := iconCatalog[name]; ok {
		return meta.Label
	}
	return name
}

// IconStyle 返回图标风格（"filled" 或 "outlined"）；未知图标返回 ok=false。
func IconStyle(name string) (string, bool) {
	meta, ok := iconCatalog[name]
	return meta.Style, ok
}

// FilterIcons 按分类 + 风格 + 关键词筛选图标名。
// category / style 为空时不过滤；keyword 匹配名称、标签或关键词（大小写不敏感）。
func FilterIcons(category, style, keyword string) []string {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	out := make([]string, 0, len(builtinIconSet))
	for name := range builtinIconSet {
		meta, ok := iconCatalog[name]
		if category != "" && (!ok || meta.Category != category) {
			continue
		}
		if style != "" && (!ok || meta.Style != style) {
			continue
		}
		if kw != "" {
			matched := strings.Contains(strings.ToLower(name), kw)
			if ok {
				matched = matched || strings.Contains(strings.ToLower(meta.Label), kw)
				for _, k := range meta.Keywords {
					if strings.Contains(strings.ToLower(k), kw) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

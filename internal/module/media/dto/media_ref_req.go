package mediadto

// AttachmentRefResp 附件的一条引用记录（构建期写入 extra_info.refs 的条目）。
type AttachmentRefResp struct {
	Kind  string `json:"kind"`            // 引用方类型：page / block / ...
	ID    string `json:"id"`              // 引用方标识
	Title string `json:"title,omitempty"` // 引用方标题（删除拦截提示展示）
}

// SyncRefsReq 构建期引用同步请求（refs 的写入侧入口）。
// 语义为「全量替换」：该引用方当前引用的 URL 集合即为最终集合，
// 不再出现的附件会被移除引用（差集增删，单条 SQL 原子更新）。
type SyncRefsReq struct {
	RefKind  string   `json:"ref_kind"`            // 引用方类型，如 page
	RefID    string   `json:"ref_id"`              // 引用方标识，如页面 ID
	RefTitle string   `json:"ref_title,omitempty"` // 引用方标题
	URLs     []string `json:"urls"`                // 该引用方产物中出现的媒体 URL
}

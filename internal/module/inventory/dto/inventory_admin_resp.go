package inventorydto

// inventory_admin_page_resp.go — 库存模块**各页根视图**（页面 GET 的回执形状）。
//
// 与同目录 inventory_page_resp.go 的分工：那一份只放「一行就是一条事实」的行元素
// （StockByWarehouseRowResp / MovementRowResp / WarehouseRowResp …），是**行**；
// 这一份放**整页的根**，是「这一屏要哪些键」的声明。
//
// 为什么行与根分开：行元素被多个页面共用（仓库行同时出现在仓库页与库存页），
// 根视图一屏一个。混在一个文件里会让「改这一页」与「改那一页」互相牵连。
//
// **导出字段名就是模板契约**：internal/templates/admin/inventory/*.html 里的
// `.Field` 必须在这里有同名导出字段 —— Jet 对缺键的处理是**整页中断**
//（HTTP 200 + 后面整块 HTML 消失），那种失败从表象几乎定位不到。

// WarehouseTypeOptionResp 仓库类型下拉的一项。
type WarehouseTypeOptionResp struct {
	Value string
	Label string
}

// WarehousesPageResp 仓库管理整页的渲染视图。
//
// 原先这里是 gin.H：键名拼错的表现是「页面 200 但少一块」，编译器一句话也不说。
type WarehousesPageResp struct {
	Title           string
	Menu            string
	Projects        []any
	SelectedProject string

	Warehouses          []WarehouseRowResp
	WarehouseTypes      []WarehouseTypeOptionResp
	WarehouseCreateForm *WarehouseFormResp
	// ListQuery 写动作表单 action 上带的筛选上下文。
	ListQuery string
}

// WarehouseFormResp 新建/编辑仓库表单的初值（模板读 .WarehouseCreateForm.X）。
type WarehouseFormResp struct {
	IsEdit          bool
	Row             *WarehouseRowResp
	SelectedProject string
	WarehouseTypes  []WarehouseTypeOptionResp
}

// ReasonFormResp 原因新建表单的初值（模板读 .SelectedProject / .Directions）。
type ReasonFormResp struct {
	SelectedProject string
	Directions      []any
}

package tree

// tree.go — 扁平列表 → 树。后台的部门、菜单、权限三处树曾经各写一份，边界语义互不相同：
// 部门树用「指针挂载 + 最后值拷贝」（注释自己记着值拷贝会丢子孙），菜单树用「按父分组再递归」，
// 权限树则先在菜单树外包一层「补齐祖先」。三份实现里只有一份意识到值拷贝问题。
//
// 合并之后固化的共同语义（三条，都有原实现作为依据）：
//
//  1. **父不在列表里的节点归入根**，不丢节点。部门树原来这样（`else { roots = append(roots, node) }`），
//     菜单树也这样（孤儿并入 `childrenOf[0]`）。区别只是「谁算根」——这里由调用方的
//     `IsRoot`/父值决定：父值为零值或父不在集合里，都算根。
//  2. **兄弟顺序 = 输入顺序**。三个调用点的输入都已按 (sort_order, id) 排好，
//     树只按输入顺序挂载，不再排序 —— 排序是查询的事。
//  3. **零值父即根**（`ParentID == 0`），与原实现一致。

// Node 树节点要提供的东西：自己的 ID、父 ID、以及「把子节点挂上来」。
//
// 用约束而不是回调：这三件事每个节点类型都必须有，写在约束里调用点就漏不了；
// 回调式 API（func(T) ID 之类）签名长，而且拼错了在编译期看不出来。
//
// **必须是 *T 实现它**（指针接收者）：SetTreeChildren 要改节点自身。
// 这也是原实现踩过的坑 —— 部门树先用 map 存指针挂载、最后再 `*root` 值拷贝出来，
// 注释里记着「值拷贝会丢子孙」。
type Node[ID comparable] interface {
	TreeID() ID
	TreeParentID() ID
	SetTreeChildren([]Node[ID])
}

// Build 把扁平列表组装成树，返回根节点列表。
//
// items 是**指针切片**（[]*T）：节点要挂子节点，值切片改了看不见。
//
// 语义见文件头三条。**环检测**是这里唯一比原实现多做的一件事：
//
//	如果数据里存在 A→B→A 这样的父引用环，朴素递归会无限展开。Jet 模板的递归渲染
//	（admin/partials/nav-nodes.html 用 {{yield navNodes(nodes=n.Children)}}）对环形数据
//	是**进程级崩溃** —— 实测 Jet v6.3.3 在自引用节点上抛
//	`fatal error: stack overflow`，那是 Go 的 fatal error，recover() 抓不住，整个进程退出。
//	后台花名册/菜单是管理员可编辑的数据，写错父级完全可能，所以这一层必须挡住。
//
// 环上的节点（以及指向它们的所有节点）会被**当作根**返回：宁可让它在列表顶层露出一次，
// 也不要让整站 500 或进程挂掉。要报告环，用 DetectCycle 单独查。
func Build[T Node[ID], ID comparable](items []T) []T {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[ID]int, len(items))
	for i := range items {
		byID[items[i].TreeID()] = i
	}

	// children[i] 是第 i 个节点的子节点下标，按输入顺序追加 —— 兄弟顺序 = 输入顺序。
	children := make([][]int, len(items))
	roots := make([]int, 0, len(items))
	for i := range items {
		pid := items[i].TreeParentID()
		var zero ID
		if pid == zero {
			roots = append(roots, i)
			continue
		}
		parentIdx, ok := byID[pid]
		if !ok || parentIdx == i {
			// 父不在列表里（被过滤掉 / 已软删）或是自己指向自己：归入根，不丢节点。
			roots = append(roots, i)
			continue
		}
		children[parentIdx] = append(children[parentIdx], i)
	}

	// 环检测：从根出发走不到的节点就是环上的（或挂在环下面的）。
	// 先按「从根可达」标一遍，再把不可达的当根，避免 Build 产出一棵永远渲染不完的树。
	reachable := markReachable(items, roots, children)
	for i := range items {
		if !reachable[i] {
			roots = append(roots, i)
			// 环上节点的父链必须断开，否则挂载时又连回去。
			children[i] = nil
			reachable[i] = true
		}
	}

	out := make([]T, 0, len(roots))
	for _, i := range roots {
		out = append(out, buildSubtree(items, children, i))
	}
	return out
}

// markReachable 从根做广度遍历，标出所有能到达的节点。
func markReachable[T Node[ID], ID comparable](items []T, roots []int, children [][]int) []bool {
	seen := make([]bool, len(items))
	queue := make([]int, 0, len(items))
	for _, r := range roots {
		if !seen[r] {
			seen[r] = true
			queue = append(queue, r)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, c := range children[queue[i]] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
	}
	return seen
}

// buildSubtree 深拷贝式构建子树。
//
// 用返回值而不是「指针挂载 + 末尾值拷贝」：后者正是部门树注释里那个坑 ——
// 先把子节点挂到 map 里的指针上，最后再 `*root` 拷出来，拷贝完成前后看到的 Children 不同。
// 这里自底向上构造，每个节点只构造一次，没有「什么时候拷」的问题。
func buildSubtree[T Node[ID], ID comparable](items []T, children [][]int, idx int) T {
	node := items[idx]
	if len(children[idx]) == 0 {
		node.SetTreeChildren(nil)
		return node
	}
	kids := make([]Node[ID], 0, len(children[idx]))
	for _, c := range children[idx] {
		kids = append(kids, buildSubtree(items, children, c))
	}
	node.SetTreeChildren(kids)
	return node
}

// DetectCycle 报出一个环（从某个节点沿父链回到自己），没有环时返回空。
//
// 与 Build 分开：Build 只保证「不崩」，它不解释数据哪里坏了。管理界面要提示
// 「A 和 B 互为父级」时用这个，拿到的是一条具体可展示的链。
func DetectCycle[T Node[ID], ID comparable](items []T) []ID {
	parentOf := make(map[ID]ID, len(items))
	for i := range items {
		parentOf[items[i].TreeID()] = items[i].TreeParentID()
	}
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[ID]int, len(items))
	for i := range items {
		start := items[i].TreeID()
		if state[start] != unvisited {
			continue
		}
		var path []ID
		cur := start
		for {
			if state[cur] == done {
				break
			}
			if state[cur] == visiting {
				// 找到环：从 path 里第一次出现 cur 的位置截到末尾。
				for j, id := range path {
					if id == cur {
						return append(path[j:], cur)
					}
				}
				return append(path, cur)
			}
			state[cur] = visiting
			path = append(path, cur)
			pid, ok := parentOf[cur]
			if !ok {
				break
			}
			var zero ID
			if pid == zero {
				break
			}
			cur = pid
		}
		for _, id := range path {
			state[id] = done
		}
	}
	return nil
}

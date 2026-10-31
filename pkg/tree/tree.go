package tree

type Node[ID comparable] interface {
	TreeID() ID
	TreeParentID() ID
	SetTreeChildren([]Node[ID])
}

func Build[T Node[ID], ID comparable](items []T) []T {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[ID]int, len(items))
	for i := range items {
		byID[items[i].TreeID()] = i
	}

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

			roots = append(roots, i)
			continue
		}
		children[parentIdx] = append(children[parentIdx], i)
	}

	reachable := markReachable(items, roots, children)
	for i := range items {
		if !reachable[i] {
			roots = append(roots, i)

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

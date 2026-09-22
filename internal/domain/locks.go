package domain

import (
	"fmt"
	"sort"
)

// LockConflict 表示新增锦标将导致成环；携带一条具体冲突链。
type LockConflict struct {
	Chain []ChainStep
}

func (c *LockConflict) Error() string {
	s := "锦标成环："
	for i, st := range c.Chain {
		if i > 0 {
			s += " → "
		}
		s += st.Detail
	}
	return s
}

// graph 节点为 (section,horizon)，边为：
//   - order 边：同剖面内深度更浅者在上（from 更深 → to 更浅，表示时间先后）
//   - lock 边：既有锦标（双向等价）
type node struct{ section, name string }

type edge struct {
	to     node
	kind   string // "order" | "lock"
	lockID int64
	detail string
}

// CheckLock 校验新增锦标 (aSec,aName)≡(bSec,bName)。
// 若成环，返回携带具体冲突链的 *LockConflict；不删除任何既有锦标。
func CheckLock(horizons []Horizon, locks []Lock, aSec, aName, bSec, bName string) *LockConflict {
	depth := map[node]float64{}
	for _, h := range horizons {
		depth[node{h.Section, h.Name}] = h.Depth
	}
	a := node{aSec, aName}
	b := node{bSec, bName}
	if _, ok := depth[a]; !ok {
		return &LockConflict{Chain: []ChainStep{{Kind: "error", Detail: fmt.Sprintf("层位 %s 不存在", horizonRef(aSec, aName))}}}
	}
	if _, ok := depth[b]; !ok {
		return &LockConflict{Chain: []ChainStep{{Kind: "error", Detail: fmt.Sprintf("层位 %s 不存在", horizonRef(bSec, bName))}}}
	}

	adj := map[node][]edge{}
	addEdge := func(u, v node, kind string, lockID int64, detail string) {
		adj[u] = append(adj[u], edge{to: v, kind: kind, lockID: lockID, detail: detail})
	}
	// 剖面内层序：深 → 浅（时间由老到新）。
	bySection := map[string][]Horizon{}
	for _, h := range horizons {
		bySection[h.Section] = append(bySection[h.Section], h)
	}
	for sec, hs := range bySection {
		sort.Slice(hs, func(i, j int) bool { return hs[i].Depth > hs[j].Depth })
		for i := 0; i+1 < len(hs); i++ {
			u := node{sec, hs[i].Name}
			v := node{sec, hs[i+1].Name}
			addEdge(u, v, "order", 0, fmt.Sprintf("%s 深于 %s（%s 剖面层序）", horizonRef(u.section, u.name), horizonRef(v.section, v.name), sec))
		}
	}
	// 既有锦标：双向等价边。
	for _, l := range locks {
		u := node{l.ASection, l.AHorizon}
		v := node{l.BSection, l.BHorizon}
		d := fmt.Sprintf("%s ≡ %s（锦标#%d）", horizonRef(u.section, u.name), horizonRef(v.section, v.name), l.ID)
		addEdge(u, v, "lock", l.ID, d)
		addEdge(v, u, "lock", l.ID, fmt.Sprintf("%s ≡ %s（锦标#%d）", horizonRef(v.section, v.name), horizonRef(u.section, u.name), l.ID))
	}

	// 若已存在 a → b 或 b → a 的时序路径，则新锦标把“先后”强行等同，闭合为环。
	if path, ok := bfs(adj, a, b); ok {
		return &LockConflict{Chain: append(path, ChainStep{
			Kind: "new_lock", FromRef: horizonRef(aSec, aName), ToRef: horizonRef(bSec, bName),
			Detail: fmt.Sprintf("新锦标断言 %s ≡ %s，与上述先后关系成环", horizonRef(aSec, aName), horizonRef(bSec, bName)),
		})}
	}
	if path, ok := bfs(adj, b, a); ok {
		return &LockConflict{Chain: append(path, ChainStep{
			Kind: "new_lock", FromRef: horizonRef(bSec, bName), ToRef: horizonRef(aSec, aName),
			Detail: fmt.Sprintf("新锦标断言 %s ≡ %s，与上述先后关系成环", horizonRef(bSec, bName), horizonRef(aSec, aName)),
		})}
	}
	return nil
}

// bfs 寻找 from → to 的有向路径，返回具体步骤链。
func bfs(adj map[node][]edge, from, to node) ([]ChainStep, bool) {
	if from == to {
		return nil, false
	}
	prev := map[node]edge{}
	seen := map[node]bool{from: true}
	queue := []node{from}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, e := range adj[u] {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			prev[e.to] = edge{to: u, kind: e.kind, lockID: e.lockID, detail: e.detail}
			if e.to == to {
				var chain []ChainStep
				for cur := to; cur != from; {
					pe := prev[cur]
					chain = append([]ChainStep{{
						Kind: pe.kind, FromRef: horizonRef(pe.to.section, pe.to.name),
						ToRef: horizonRef(cur.section, cur.name), LockID: pe.lockID, Detail: pe.detail,
					}}, chain...)
					cur = pe.to
				}
				return chain, true
			}
			queue = append(queue, e.to)
		}
	}
	return nil, false
}

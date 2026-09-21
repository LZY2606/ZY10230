package engine

import (
	"fmt"
	"sort"
)

// globalScheme 用锦标把多剖面层位并置。成环时返回具体冲突链，
// 不删除任何锦标；参与环的锦标标记为 conflicted（持久化），但保留可恢复。
func globalScheme(s *snapshot, horizons []Horizon, interp string, cycleOverride []string) (Scheme, []string) {
	ignore := map[string]bool{}
	for _, id := range cycleOverride {
		ignore[id] = true
	}
	g, unresolved := buildTieGraph(horizons, s.ties, interp, ignore)
	cycle := findCycle(g)
	sc := Scheme{
		ID:             "scheme|global|" + interp,
		Title:          "跨剖面对锦标齐 · " + interpLabel(interp),
		Interpretation: interp, Scope: "global",
	}
	sc.Violations = append(sc.Violations, unresolved...)
	if len(cycle) > 0 {
		ids := tieIDsInChain(cycle)
		sc.ConflictChain = chainSteps(cycle)
		sc.Violations = append(sc.Violations, Violation{
			Code: "tie_cycle", Severity: "hard",
			Message: fmt.Sprintf("对齐锦标成环：沿 %d 条层位序边与锦标 %v 回到起点；系统不删除任何锦标，请人工解除或改挂",
				len(cycle), ids),
			Refs: tieRefs(ids),
		})
		sort.Slice(sc.Violations, func(i, j int) bool { return sc.Violations[i].Code < sc.Violations[j].Code })
		return sc, ids
	}
	// 无环：以层位序边求拓扑层级，并集上等价层位，输出全局并置组合带。
	zones := globalZones(g)
	sc.Zones = zones
	return sc, nil
}

func tieRefs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, "tie:"+id)
	}
	sort.Strings(out)
	return out
}

func globalZones(g *horizonGraph) []Zone {
	if len(g.nodes) == 0 {
		return nil
	}
	// 并查集：锦标等价的层位合并
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if parent[x] == "" {
			parent[x] = x
		}
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	// 收集 tie 等价关系（adj 双向）
	seenPair := map[string]bool{}
	for u, es := range g.adj {
		for _, e := range es {
			if e.edge != "tie" {
				continue
			}
			key := u + "|" + e.to
			rkey := e.to + "|" + u
			if seenPair[key] || seenPair[rkey] {
				continue
			}
			seenPair[key] = true
			union(u, e.to)
		}
	}
	// 组件深度（各节点深度均值仅用于排序提示）
	type comp struct {
		root  string
		nodes []string
	}
	compOf := map[string]*comp{}
	for id := range g.nodes {
		r := find(id)
		if compOf[r] == nil {
			compOf[r] = &comp{root: r}
		}
		compOf[r].nodes = append(compOf[r].nodes, id)
	}
	// order 边转化为组件间 DAG，求最长路径层级
	indeg := map[string]int{}
	dag := map[string]map[string]bool{}
	for u, es := range g.adj {
		for _, e := range es {
			if e.edge != "order" {
				continue
			}
			cu, cv := find(u), find(e.to)
			if cu == cv {
				continue // 已在成环阶段拦截；无环时不应出现
			}
			if dag[cu] == nil {
				dag[cu] = map[string]bool{}
			}
			if !dag[cu][cv] {
				dag[cu][cv] = true
				indeg[cv]++
				if _, ok := indeg[cu]; !ok {
					indeg[cu] += 0
				}
			}
		}
	}
	level := map[string]int{}
	var queue []string
	for r := range compOf {
		if indeg[r] == 0 {
			queue = append(queue, r)
			level[r] = 0
		}
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for v := range dag[u] {
			if level[u]+1 > level[v] {
				level[v] = level[u] + 1
			}
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	var roots []string
	for r := range compOf {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool {
		if level[roots[i]] != level[roots[j]] {
			return level[roots[i]] < level[roots[j]]
		}
		return roots[i] < roots[j]
	})
	var zones []Zone
	for idx, r := range roots {
		var groups []string
		seenG := map[string]bool{}
		for _, nid := range compOf[r].nodes {
			n := g.nodes[nid]
			if !seenG[n.g] {
				seenG[n.g] = true
				groups = append(groups, n.g)
			}
		}
		sort.Strings(groups)
		zones = append(zones, Zone{
			Index:  idx + 1,
			Name:   "全球并置带 " + roman(idx+1) + "（" + joinGroups(groups) + "）",
			Groups: groups,
		})
	}
	return zones
}

func joinGroups(gs []string) string {
	labels := make([]string, 0, len(gs))
	for _, x := range gs {
		labels = append(labels, x)
	}
	return joinComma(labels)
}

func joinComma(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

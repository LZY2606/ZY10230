package engine

import (
	"fmt"
	"sort"
	"strings"
)

type horizonNode struct {
	id    string // group#kind@section
	g     string
	kind  string
	sec   string
	depth float64
}

type graphEdge struct {
	from, to string
	edge     string // order | tie
	tieID    string
	Detail   string
}

type horizonGraph struct {
	nodes map[string]*horizonNode
	// adj 为有向邻接：order 边老→新（深→浅），tie 边为双向。
	adj map[string][]graphEdge
}

func horizonID(g, kind, sec string) string { return g + "#" + kind + "@" + sec }

// buildTieGraph 按指定解释构建层位图：同剖面按深度（老→新）连 order 边，
// 锦标按等时关系连双向 tie 边。无法解析的锦标端点被跳过。
func buildTieGraph(horizons []Horizon, ties []Tie, interp string, ignoreTie map[string]bool) (*horizonGraph, []Violation) {
	g := &horizonGraph{nodes: map[string]*horizonNode{}, adj: map[string][]graphEdge{}}
	depthOf := map[string]float64{}
	for _, h := range horizons {
		var d *float64
		if interp == "raw" {
			d = h.DepthRaw
		} else {
			d = h.DepthIn
			if d == nil {
				d = h.DepthRaw // in_situ 无产出时不建节点，下面会排除
			}
		}
		if interp == "in_situ" && h.DepthIn == nil {
			continue
		}
		if d == nil {
			continue
		}
		id := horizonID(h.GroupID, h.Kind, h.SectionID)
		g.nodes[id] = &horizonNode{id: id, g: h.GroupID, kind: h.Kind, sec: h.SectionID, depth: *d}
		depthOf[id] = *d
	}
	// 同剖面层位：深（老）→浅（新）
	bySec := map[string][]string{}
	for id := range g.nodes {
		bySec[g.nodes[id].sec] = append(bySec[g.nodes[id].sec], id)
	}
	for _, ids := range bySec {
		sort.Slice(ids, func(i, j int) bool { return depthOf[ids[i]] > depthOf[ids[j]] })
		for i := 0; i+1 < len(ids); i++ {
			if depthOf[ids[i]] == depthOf[ids[i+1]] {
				continue
			}
			a, b := ids[i], ids[i+1]
			g.adj[a] = append(g.adj[a], graphEdge{from: a, to: b, edge: "order",
				Detail: fmt.Sprintf("同剖面深度序 %s(%s) 老于 %s(%s)", a, f2s(depthOf[a]), b, f2s(depthOf[b]))})
		}
	}
	var unresolved []Violation
	for _, t := range ties {
		if !t.Active || ignoreTie[t.ID] {
			continue
		}
		a := horizonID(t.AGroup, t.AKind, t.ASection)
		b := horizonID(t.BGroup, t.BKind, t.BSection)
		_, okA := g.nodes[a]
		_, okB := g.nodes[b]
		if !okA || !okB {
			unresolved = append(unresolved, Violation{
				Code: "unresolvable_tie", Severity: "soft",
				Message: fmt.Sprintf("锦标 %s 的端点在%s解释下无法全部解析（%s=%v, %s=%v），不参与该解释的成环判定",
					t.ID, interpLabel(interp), a, okA, b, okB),
				Refs: []string{"tie:" + t.ID},
			})
			continue
		}
		g.adj[a] = append(g.adj[a], graphEdge{from: a, to: b, edge: "tie", tieID: t.ID,
			Detail: fmt.Sprintf("锦标 %s 锁定 %s ≡ %s", t.ID, a, b)})
		g.adj[b] = append(g.adj[b], graphEdge{from: b, to: a, edge: "tie", tieID: t.ID,
			Detail: fmt.Sprintf("锦标 %s 锁定 %s ≡ %s", t.ID, a, b)})
	}
	return g, unresolved
}

// findCycle 返回一条具体有向环（边序列），无环返回 nil。
// 沿同一条锦标立即原路返回（tie 反向边）不算成环；
// 纯锦标三角不含层位序约束，也不构成地质时序冲突。
func findCycle(g *horizonGraph) []graphEdge {
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	var stack []string
	var edgeStack []graphEdge
	var found []graphEdge
	var dfs func(u string, fromTie string) bool
	dfs = func(u string, fromTie string) bool {
		color[u] = gray
		stack = append(stack, u)
		neighbors := append([]graphEdge{}, g.adj[u]...)
		sort.Slice(neighbors, func(i, j int) bool {
			if neighbors[i].to != neighbors[j].to {
				return neighbors[i].to < neighbors[j].to
			}
			return neighbors[i].tieID < neighbors[j].tieID
		})
		for _, e := range neighbors {
			if e.edge == "tie" && e.tieID != "" && e.tieID == fromTie {
				continue
			}
			nextFrom := ""
			if e.edge == "tie" {
				nextFrom = e.tieID
			}
			if color[e.to] == white {
				edgeStack = append(edgeStack, e)
				if dfs(e.to, nextFrom) {
					return true
				}
				edgeStack = edgeStack[:len(edgeStack)-1]
			} else if color[e.to] == gray {
				start := 0
				for stack[start] != e.to {
					start++
				}
				cand := append([]graphEdge{}, edgeStack[start:]...)
				cand = append(cand, e)
				hasOrder := false
				for _, ce := range cand {
					if ce.edge == "order" {
						hasOrder = true
					}
				}
				if !hasOrder {
					continue
				}
				found = cand
				return true
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return false
	}
	var nodes []string
	for id := range g.nodes {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if color[n] == white {
			if dfs(n, "") {
				return found
			}
		}
	}
	return nil
}

func chainSteps(edges []graphEdge) []ChainStep {
	var out []ChainStep
	for _, e := range edges {
		out = append(out, ChainStep{From: e.from, To: e.to, Edge: e.edge, TieID: e.tieID, Detail: e.Detail})
	}
	return out
}

func tieIDsInChain(edges []graphEdge) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range edges {
		if e.edge == "tie" && !seen[e.tieID] {
			seen[e.tieID] = true
			out = append(out, e.tieID)
		}
	}
	sort.Strings(out)
	return out
}

// perSectionSchemes 为每条剖面、每种解释生成事件分带（按 FAD/LAD 深度切片）。
func perSectionSchemes(s *snapshot, events []computedEvent, horizons []Horizon) []Scheme {
	var out []Scheme
	for _, interp := range []string{"raw", "in_situ"} {
		for _, sec := range s.sections {
			type bnd struct {
				depth float64
				group string
				kind  string
			}
			var bnds []bnd
			groupsHere := map[string]bool{}
			for _, h := range horizons {
				if h.SectionID != sec.ID {
					continue
				}
				var d *float64
				if interp == "raw" {
					d = h.DepthRaw
				} else {
					d = h.DepthIn
				}
				if d == nil {
					continue
				}
				bnds = append(bnds, bnd{*d, h.GroupID, h.Kind})
				groupsHere[h.GroupID] = true
			}
			if len(bnds) == 0 {
				continue
			}
			sort.Slice(bnds, func(i, j int) bool {
				if bnds[i].depth != bnds[j].depth {
					return bnds[i].depth < bnds[j].depth
				}
				return bnds[i].group+bnds[i].kind < bnds[j].group+bnds[j].kind
			})
			// 由边界构造共存组合带
			active := map[string]bool{}
			var zones []Zone
			var prevDepth float64
			start := true
			flush := func(top, base float64) {
				var gs []string
				for g := range active {
					gs = append(gs, g)
				}
				sort.Strings(gs)
				if len(gs) == 0 {
					return
				}
				t, b := top, base
				zones = append(zones, Zone{
					Index: len(zones) + 1,
					Name:  "组合带 " + roman(len(zones)+1) + "（" + strings.Join(gs, "+") + "）",
					Top:   &t, Base: &b, Groups: gs, Section: sec.ID,
				})
			}
			for _, b := range bnds {
				if !start {
					flush(prevDepth, b.depth)
				}
				start = false
				if b.kind == "FAD" {
					active[b.group] = true
				} else {
					delete(active, b.group)
				}
				prevDepth = b.depth
			}
			// 最后一段：仍有活跃类群则保留开放底界
			if len(active) > 0 {
				var gs []string
				for g := range active {
					gs = append(gs, g)
				}
				sort.Strings(gs)
				t := prevDepth
				zones = append(zones, Zone{
					Index: len(zones) + 1,
					Name:  "组合带 " + roman(len(zones)+1) + "（" + strings.Join(gs, "+") + "）",
					Top:   &t, Base: nil, Groups: gs, Section: sec.ID,
				})
			}
			out = append(out, Scheme{
				ID:             "scheme|section|" + interp + "|" + sec.ID,
				Title:          sec.Name + " · " + interpLabel(interp) + "事件分带",
				Interpretation: interp, Scope: "per-section", SectionID: sec.ID,
				Zones:      zones,
				Violations: sectionViolations(s, sec.ID, interp, events, horizons),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Interpretation != out[j].Interpretation {
			return out[i].Interpretation < out[j].Interpretation
		}
		return out[i].SectionID < out[j].SectionID
	})
	return out
}

func roman(n int) string {
	romans := []struct {
		v int
		s string
	}{{10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}}
	var b strings.Builder
	for _, r := range romans {
		for n >= r.v {
			b.WriteString(r.s)
			n -= r.v
		}
	}
	return b.String()
}

func interpLabel(i string) string {
	if i == "raw" {
		return "原始口径(含重工点)"
	}
	return "原位口径(剔除重工候选)"
}

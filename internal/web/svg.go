package web

import (
	"fmt"
	"net/http"

	"fossilcourt/internal/engine"
)

// handleSVG 输出服务端生成的 SVG：三条剖面深度柱，
// 绿色=正常产出，橙色=重工候选，灰色=采样层段，红/蓝线=FAD/LAD。
func (s *Server) handleSVG(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.State()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Write([]byte(renderSVG(st)))
}

const svgHeader = `<svg xmlns="http://www.w3.org/2000/svg" width="900" height="520" viewBox="0 0 900 520" font-family="Helvetica,Arial,sans-serif">`

func renderSVG(st *engine.State) string {
	var maxD, minD float64 = 0, 1e9
	for _, iv := range st.Intervals {
		if iv.BaseDepth > maxD {
			maxD = iv.BaseDepth
		}
		if iv.TopDepth < minD {
			minD = iv.TopDepth
		}
	}
	if minD == 1e9 {
		minD = 0
	}
	top, height := 50.0, 420.0
	yOf := func(d float64) float64 {
		return top + (d-minD)/(maxD-minD)*height
	}
	colW := 250.0
	out := svgHeader
	out += `<rect width="900" height="520" fill="#faf7f0"/>`
	out += `<text x="24" y="30" font-size="20" font-weight="bold" fill="#3a2e20">化石带议庭 · 剖面深度与产出（深度向下增大）</text>`
	for i, sec := range st.Sections {
		x := 40 + float64(i)*colW
		out += fmt.Sprintf(`<text x="%g" y="46" font-size="15" font-weight="bold" fill="#3a2e20">%s（%s）</text>`, x, sec.Name, sec.ID)
		// 采样层段
		for _, iv := range st.Intervals {
			if iv.SectionID != sec.ID {
				continue
			}
			fill := "#d8d2c4"
			op := "0.55"
			if !iv.Sufficient {
				fill = "#e8ddc8"
				op = "0.35"
			}
			y, h := yOf(iv.TopDepth), yOf(iv.BaseDepth)-yOf(iv.TopDepth)
			out += fmt.Sprintf(`<rect x="%g" y="%g" width="150" height="%g" fill="%s" opacity="%s" stroke="#8a7f6a"/>`, x+30, y, h, fill, op)
			suff := "充分"
			if !iv.Sufficient {
				suff = "不足"
			}
			out += fmt.Sprintf(`<text x="%g" y="%g" font-size="9" fill="#6b5f4b">%g-%g %s</text>`, x+186, y+h/2+3, iv.TopDepth, iv.BaseDepth, suff)
		}
		// 产出
		for _, o := range st.Occurrences {
			if o.SectionID != sec.ID {
				continue
			}
			y := yOf(o.Depth)
			if o.Status == "absent" {
				out += fmt.Sprintf(`<circle cx="%g" cy="%g" r="4" fill="none" stroke="#7a6f5a" stroke-dasharray="2,2"/>`, x+50, y)
				continue
			}
			color := "#2f7d46"
			if o.Rework {
				color = "#d97a1f"
			}
			txName := o.TaxonID
			out += fmt.Sprintf(`<circle cx="%g" cy="%g" r="5" fill="%s"/>`, x+50, y, color)
			out += fmt.Sprintf(`<text x="%g" y="%g" font-size="9" fill="#333">%s</text>`, x+60, y+3, txName)
		}
		// FAD/LAD 线（原始口径）
		for _, ev := range st.Events {
			if ev.SectionID != sec.ID || ev.Interpretation != "raw" || ev.Depth == nil {
				continue
			}
			color, dash := "#1f5fbf", ""
			if ev.Kind == "LAD" {
				color, dash = "#b03030", "4,3"
			} else if ev.Kind != "FAD" {
				continue
			}
			y := yOf(*ev.Depth)
			out += fmt.Sprintf(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.4" stroke-dasharray="%s"/>`, x+30, y, x+180, y, color, dash)
			out += fmt.Sprintf(`<text x="%g" y="%g" font-size="8" fill="%s">%s %s</text>`, x+32, y-2, color, ev.Kind, ev.GroupID)
		}
		// 深度轴
		out += fmt.Sprintf(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#5a4f3b"/>`, x+30, top, x+30, top+height)
		out += fmt.Sprintf(`<text x="%g" y="%g" font-size="9" fill="#5a4f3b">%g</text>`, x+4, top+4, minD)
		out += fmt.Sprintf(`<text x="%g" y="%g" font-size="9" fill="#5a4f3b">%g</text>`, x+4, top+height+4, maxD)
	}
	out += `<g font-size="10" fill="#3a2e20">
		<circle cx="40" cy="500" r="5" fill="#2f7d46"/><text x="50" y="504">正常产出</text>
		<circle cx="120" cy="500" r="5" fill="#d97a1f"/><text x="130" y="504">重工候选</text>
		<circle cx="200" cy="500" r="4" fill="none" stroke="#7a6f5a" stroke-dasharray="2,2"/><text x="210" y="504">充分采样中未见</text>
		<line x1="320" y1="500" x2="345" y2="500" stroke="#1f5fbf"/><text x="349" y="504">FAD</text>
		<line x1="390" y1="500" x2="415" y2="500" stroke="#b03030" stroke-dasharray="4,3"/><text x="419" y="504">LAD</text>
	</g>`
	out += `</svg>`
	return out
}

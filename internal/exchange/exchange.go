// Package exchange 负责运行记录导出（JSON bundle）与清空后重新导入复核。
package exchange

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"fossilcourt/internal/store"
)

type Bundle struct {
	FormatVersion   int   `json:"formatVersion"`
	SchemaVersion   int   `json:"schemaVersion"`
	Sections        []Row `json:"sections"`
	Taxa            []Row `json:"taxa"`
	SampleIntervals []Row `json:"sampleIntervals"`
	Occurrences     []Row `json:"occurrences"`
	SynVersions     []Row `json:"synVersions"`
	SynGroups       []Row `json:"synGroups"`
	SynMembers      []Row `json:"synMembers"`
	Rework          []Row `json:"reworkCandidates"`
	Ties            []Row `json:"ties"`
	Events          []Row `json:"events"`
	EventEvidence   []Row `json:"eventEvidence"`
	RecomputeLog    []Row `json:"recomputeLog"`
	Runs            []Row `json:"runs"`
}

type Row struct {
	Cols []string `json:"cols"`
	Vals []any    `json:"vals"`
}

var tables = []struct {
	name string
	cols []string
}{
	{"sections", []string{"id", "name", "ord", "depth_unit"}},
	{"taxa", []string{"id", "name"}},
	{"sample_intervals", []string{"id", "section_id", "top_depth", "base_depth", "sufficient", "note"}},
	{"occurrences", []string{"id", "section_id", "taxon_id", "depth", "status", "interval_id", "note"}},
	{"syn_versions", []string{"version", "created_at", "kind", "group_id", "display_name", "members", "note"}},
	{"syn_groups", []string{"id", "display_name"}},
	{"syn_members", []string{"group_id", "taxon_id"}},
	{"rework_candidates", []string{"occurrence_id", "marked", "updated_at", "note"}},
	{"ties", []string{"id", "created_at", "a_group", "a_section", "a_kind", "b_group", "b_section", "b_kind", "locked", "active", "conflicted", "note"}},
	{"events", []string{"id", "revision", "interpretation", "kind", "group_id", "section_id", "depth", "partner_group", "updated_at"}},
	{"event_evidence", []string{"event_id", "occurrence_id", "interval_id", "role", "note"}},
	{"recompute_log", []string{"id", "at", "cause", "scope", "recomputed", "preserved", "detail"}},
	{"runs", []string{"id", "at", "kind", "payload"}},
}

// Export 全量导出（含事件与重算日志，即“运行记录”）。
func Export(db *store.DB) (*Bundle, error) {
	b := &Bundle{FormatVersion: 1, SchemaVersion: store.SchemaVersion}
	for _, t := range tables {
		rows, err := db.Sql.Query(`SELECT ` + joinCols(t.cols) + ` FROM ` + t.name)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", t.name, err)
		}
		var out []Row
		for rows.Next() {
			vals := make([]any, len(t.cols))
			ptrs := make([]any, len(t.cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			for i := range vals {
				vals[i] = normalize(vals[i])
			}
			out = append(out, Row{Cols: append([]string{}, t.cols...), Vals: vals})
		}
		rows.Close()
		sort.Slice(out, func(i, j int) bool {
			return fmt.Sprint(out[i].Vals[0]) < fmt.Sprint(out[j].Vals[0])
		})
		switch t.name {
		case "sections":
			b.Sections = out
		case "taxa":
			b.Taxa = out
		case "sample_intervals":
			b.SampleIntervals = out
		case "occurrences":
			b.Occurrences = out
		case "syn_versions":
			b.SynVersions = out
		case "syn_groups":
			b.SynGroups = out
		case "syn_members":
			b.SynMembers = out
		case "rework_candidates":
			b.Rework = out
		case "ties":
			b.Ties = out
		case "events":
			b.Events = out
		case "event_evidence":
			b.EventEvidence = out
		case "recompute_log":
			b.RecomputeLog = out
		case "runs":
			b.Runs = out
		}
	}
	return b, nil
}

func normalize(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case int64:
		return x
	case nil:
		return nil
	default:
		return x
	}
}

// Import 清空所有数据表后导入 bundle（schema 保留）。
func Import(db *store.DB, b *Bundle) error {
	tx, err := db.Sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 外键开启时必须按依赖逆序清空
	delOrder := []string{
		"runs", "event_evidence", "events", "ties", "rework_candidates",
		"recompute_log", "syn_members", "syn_versions", "syn_groups",
		"occurrences", "sample_intervals", "taxa", "sections",
	}
	for _, name := range delOrder {
		if _, err := tx.Exec(`DELETE FROM ` + name); err != nil {
			return err
		}
	}
	if err := importRows(tx, "sections", b.Sections); err != nil {
		return err
	}
	if err := importRows(tx, "taxa", b.Taxa); err != nil {
		return err
	}
	if err := importRows(tx, "sample_intervals", b.SampleIntervals); err != nil {
		return err
	}
	if err := importRows(tx, "syn_groups", b.SynGroups); err != nil {
		return err
	}
	if err := importRows(tx, "syn_members", b.SynMembers); err != nil {
		return err
	}
	if err := importRows(tx, "syn_versions", b.SynVersions); err != nil {
		return err
	}
	if err := importRows(tx, "occurrences", b.Occurrences); err != nil {
		return err
	}
	if err := importRows(tx, "rework_candidates", b.Rework); err != nil {
		return err
	}
	if err := importRows(tx, "ties", b.Ties); err != nil {
		return err
	}
	if err := importRows(tx, "events", b.Events); err != nil {
		return err
	}
	if err := importRows(tx, "event_evidence", b.EventEvidence); err != nil {
		return err
	}
	if err := importRows(tx, "recompute_log", b.RecomputeLog); err != nil {
		return err
	}
	if err := importRows(tx, "runs", b.Runs); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta(key,value) VALUES('schema_version',?)`, store.SchemaVersion); err != nil {
		return err
	}
	// 显式自增 ID 随 bundle 恢复；SQLite 会在显式插入较大值后自动推进 sqlite_sequence，
	// 但为保证跨版本/跨库一致，这里直接把序列对齐到表内最大值。
	for _, t := range []string{"syn_versions", "recompute_log", "runs"} {
		if _, err := tx.Exec(`DELETE FROM sqlite_sequence WHERE name=?`, t); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sqlite_sequence(name,seq)
			SELECT ?, COALESCE(MAX(`+seqCol(t)+`),0) FROM `+t, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func importRows(tx *sql.Tx, table string, rows []Row) error {
	colsByTable := map[string][]string{}
	for _, t := range tables {
		colsByTable[t.name] = t.cols
	}
	cols := colsByTable[table]
	for _, r := range rows {
		if len(r.Vals) != len(cols) {
			return fmt.Errorf("table %s 行列数不匹配: %d != %d", table, len(r.Vals), len(cols))
		}
		q := `INSERT INTO ` + table + `(` + joinCols(cols) + `) VALUES(` + placeholders(len(cols)) + `)`
		vals := make([]any, len(cols))
		for i, v := range r.Vals {
			vals[i] = coerce(v)
		}
		if _, err := tx.Exec(q, vals...); err != nil {
			return fmt.Errorf("import %s: %w", table, err)
		}
	}
	return nil
}

func coerce(v any) any {
	// SQLite 驱动接受 float64/int64/string/nil
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	default:
		return v
	}
}

func joinCols(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out
}

func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}

func seqCol(table string) string {
	switch table {
	case "syn_versions":
		return "version"
	default:
		return "id"
	}
}

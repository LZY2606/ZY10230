package engine_test

import (
	"path/filepath"
	"testing"

	"fossilcourt/internal/engine"
	"fossilcourt/internal/fixture"
	"fossilcourt/internal/store"
)

func newSvc(t *testing.T) (*engine.Service, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Sql.Close() })
	svc := engine.NewService(db)
	if err := svc.SeedData(fixture.Fixed()); err != nil {
		t.Fatal(err)
	}
	return svc, db
}

func state(t *testing.T, svc *engine.Service) *engine.State {
	t.Helper()
	st, err := svc.State()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func statusOf(st *engine.State, sec, tax string) string {
	for _, s := range st.Statuses {
		if s.SectionID == sec && s.TaxonID == tax {
			return s.State
		}
	}
	return ""
}

// 验收 1：同一分类单元在三条剖面分别为正常产出 / 重工高位点 / 完全未采样；
// 且“未见”(充分采样负证据) 与“未采样”严格区分。
func TestAbsentIsNotUnsampled(t *testing.T) {
	svc, _ := newSvc(t)
	st := state(t, svc)

	// G-T 成员 TA：S1 正常产出；TB：S2 正常产出但含重工高位点；G-T 在 S3 完全未采样
	if got := statusOf(st, "S1", "TA"); got != "present" {
		t.Fatalf("TA@S1 = %q, want present", got)
	}
	if got := statusOf(st, "S2", "TB"); got != "present" {
		t.Fatalf("TB@S2 = %q, want present", got)
	}
	if got := statusOf(st, "S3", "TA"); got != "unsampled" {
		t.Fatalf("TA@S3 = %q, want unsampled", got)
	}
	if got := statusOf(st, "S3", "TB"); got != "unsampled" {
		t.Fatalf("TB@S3 = %q, want unsampled", got)
	}
	// W：在 S3 的充分采样层段中登记“未见” → absent（可作负证据），不是 unsampled
	if got := statusOf(st, "S3", "W"); got != "absent" {
		t.Fatalf("W@S3 = %q, want absent(negative evidence)", got)
	}
	// S3 没有任何不充分层段中的 W 记录，不应出现 insufficient；再核对 U 正常
	if got := statusOf(st, "S3", "U"); got != "present" {
		t.Fatalf("U@S3 = %q, want present", got)
	}

	// 事件层：TA/TB 组 G-T 在 S3 不得有 FAD/LAD（未采样不能推出任何事件）
	for _, ev := range st.Events {
		if ev.SectionID == "S3" && ev.GroupID == "G-T" && ev.Kind != "COEX" {
			t.Fatalf("G-T 在完全未采样的 S3 不应产生 %s 事件: %+v", ev.Kind, ev)
		}
	}
}

// 重工点：raw 与 in_situ 两种解释并存，且导致 S2 的 FAD 顺序倒置（不按最值硬定）。
func TestReworkKeepsMultipleInterpretations(t *testing.T) {
	svc, _ := newSvc(t)
	st := state(t, svc)

	var rawGT, inGT, rawU, inU *float64
	for i := range st.Events {
		ev := st.Events[i]
		if ev.SectionID != "S2" || ev.Kind != "FAD" {
			continue
		}
		switch {
		case ev.GroupID == "G-T" && ev.Interpretation == "raw":
			rawGT = ev.Depth
		case ev.GroupID == "G-T" && ev.Interpretation == "in_situ":
			inGT = ev.Depth
		case ev.GroupID == "~U" && ev.Interpretation == "raw":
			rawU = ev.Depth
		case ev.GroupID == "~U" && ev.Interpretation == "in_situ":
			inU = ev.Depth
		}
	}
	if rawGT == nil || inGT == nil || rawU == nil || inU == nil {
		t.Fatalf("缺少必要 FAD 事件: gt=%v/%v u=%v/%v", rawGT, inGT, rawU, inU)
	}
	// raw: G-T FAD=60（重工点）浅于 U=100；in_situ: G-T FAD=150 深于 U=100
	if !(*rawGT == 60 && *inGT == 150 && *rawU == 100 && *inU == 100) {
		t.Fatalf("双解释深度异常: rawGT=%v inGT=%v rawU=%v inU=%v", *rawGT, *inGT, *rawU, *inU)
	}
	if (*rawGT-*rawU)*(*inGT-*inU) >= 0 {
		t.Fatal("期望 raw/in_situ 下 G-T 与 U 的 FAD 先后倒置")
	}
	// 存在 rework_ambiguity / event_order_inversion 软约束提示
	foundCodes := map[string]bool{}
	for _, sc := range st.Schemes {
		for _, v := range sc.Violations {
			foundCodes[v.Code] = true
		}
	}
	if !foundCodes["rework_ambiguity"] || !foundCodes["event_order_inversion"] {
		t.Fatalf("缺少重工/倒置软约束: %v", foundCodes)
	}
}

// 验收 2：修改等义关系后只重算依赖事件，无关事件保留。
func TestSynonymyRecomputeScoped(t *testing.T) {
	svc, _ := newSvc(t)
	before := state(t, svc)
	revBefore := map[string]int{}
	for _, ev := range before.Events {
		revBefore[ev.ID] = ev.Revision
	}

	// 把无关的两个分类单元 Q、W 合并（Q 有产出，W 无产出）
	if err := svc.MergeTaxa("G-QW", "Q/W 试验并组", []string{"Q", "W"}, "测试"); err != nil {
		t.Fatal(err)
	}
	after := state(t, svc)

	changed, preserved := 0, 0
	var unrelatedUnchanged []string
	for _, ev := range after.Events {
		dep := ev.GroupID == "G-QW" || ev.PartnerGroup == "G-QW"
		if dep {
			if ev.Revision >= 1 {
				changed++
			}
			continue
		}
		if rb, ok := revBefore[ev.ID]; ok && rb == ev.Revision {
			preserved++
		}
		// G-T 与 U 事件必须原样保留（revision 不变）
		if (ev.GroupID == "G-T" || ev.GroupID == "~U") && ev.PartnerGroup == "" {
			if revBefore[ev.ID] != ev.Revision {
				unrelatedUnchanged = append(unrelatedUnchanged, ev.ID)
			}
		}
	}
	if len(unrelatedUnchanged) > 0 {
		t.Fatalf("无关事件被重算: %v", unrelatedUnchanged)
	}
	if changed == 0 {
		t.Fatal("依赖事件应当被重算/生成")
	}
	if preserved == 0 {
		t.Fatal("非依赖事件应保留")
	}

	// 重算日志最新一条应明确范围
	logs := after.RecomputeLog
	if len(logs) == 0 || logs[0].Cause != "synonymy:merge" {
		t.Fatalf("重算日志异常: %+v", logs)
	}
	if logs[0].Preserved <= 0 {
		t.Fatalf("应记录保留事件数, got %+v", logs[0])
	}

	// 再拆分：G-QW 事件消失，G-T/U 仍保留
	if err := svc.SplitTaxa("G-QW", "测试拆分"); err != nil {
		t.Fatal(err)
	}
	final := state(t, svc)
	for _, ev := range final.Events {
		if ev.GroupID == "G-QW" || ev.PartnerGroup == "G-QW" {
			t.Fatalf("拆分后不应再出现 G-QW 事件: %s", ev.ID)
		}
	}
	var versions []int
	for _, v := range final.SynVersions {
		if v.GroupID == "G-QW" {
			versions = append(versions, v.Version)
		}
	}
	if len(versions) != 2 {
		t.Fatalf("G-QW 应有 merge+split 两个版本, got %v", versions)
	}
}

// 验收 3：加入闭合锦标后成环，返回具体冲突链，且不删除任何锦标。
func TestTieCycleReturnsConcreteChain(t *testing.T) {
	svc, _ := newSvc(t)
	st0 := state(t, svc)
	for _, ti := range st0.Ties {
		if ti.Conflicted {
			t.Fatalf("初始锦标不应成环: %s", ti.ID)
		}
	}

	// 第三条闭合锦标：U FAD@S1 ≡ U LAD@S3
	id, chain, ids, err := svc.AddTie(engine.TieInput{
		AGroup: "~U", ASection: "S1", AKind: "FAD",
		BGroup: "~U", BSection: "S3", BKind: "LAD",
		Note: "闭合锦标",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) < 3 {
		t.Fatalf("冲突链应至少包含 3 条边, got %d: %+v", len(chain), chain)
	}
	// 链必须同时含 order 边和 tie 边，并且首尾成环（最后一条边回到起点）
	hasOrder, hasTie := false, false
	for _, s := range chain {
		if s.Edge == "order" {
			hasOrder = true
		}
		if s.Edge == "tie" {
			hasTie = true
		}
		if s.Edge == "tie" && s.TieID == "" {
			t.Fatal("tie 边必须带锦标 id")
		}
	}
	if !hasOrder || !hasTie {
		t.Fatalf("冲突链必须含层位序边与锦标边: %+v", chain)
	}
	if chain[0].From != chain[len(chain)-1].To {
		t.Fatalf("冲突链未闭合: %s != %s", chain[0].From, chain[len(chain)-1].To)
	}
	// 新锦标必须包含在冲突锦标中
	foundNew := false
	for _, x := range ids {
		if x == id {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("冲突锦标集合应包含新锦标 %s: %v", id, ids)
	}

	// 关键：不通过删除最新锦标自动恢复——三条锦标都仍在，且被标记 conflicted
	st := state(t, svc)
	active := map[string]bool{}
	conflicted := map[string]bool{}
	for _, ti := range st.Ties {
		active[ti.ID] = ti.Active
		conflicted[ti.ID] = ti.Conflicted
	}
	if !active[id] {
		t.Fatal("成环后系统不得自动删除/停用最新锦标")
	}
	if !conflicted[id] {
		t.Fatal("最新锦标应被标记为 conflicted")
	}

	// 全局方案必须带 hard tie_cycle 与具体链
	sawHard := false
	for _, sc := range st.Schemes {
		if sc.Scope != "global" {
			continue
		}
		for _, v := range sc.Violations {
			if v.Code == "tie_cycle" && v.Severity == "hard" {
				sawHard = true
			}
		}
		if len(sc.ConflictChain) == 0 && len(sc.Violations) > 0 {
			t.Fatalf("成环全局方案缺少具体冲突链: %+v", sc)
		}
	}
	if !sawHard {
		t.Fatal("未在全局方案中找到 hard tie_cycle 冲突")
	}

	// 人工停用一条旧锦标后环解除（人工恢复，而非自动）
	if err := svc.DeactivateTie("TIE-SEED1"); err != nil {
		t.Fatal(err)
	}
	st2 := state(t, svc)
	for _, ti := range st2.Ties {
		if ti.Active && ti.Conflicted {
			t.Fatalf("解除后仍有活跃冲突锦标: %s", ti.ID)
		}
	}
}

// 运行记录导出 → 清空 → 重新导入后，事件与分带状态可复核一致。
func TestExportImportReplay(t *testing.T) {
	svc, db := newSvc(t)
	// 先制造一些用户操作
	if err := svc.SetRework("O-S2-1", false, "测试撤销"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRework("O-S2-1", true, "重新标记"); err != nil {
		t.Fatal(err)
	}
	bundle, err := engine.ExportForTest(db)
	if err != nil {
		t.Fatal(err)
	}
	stBefore := state(t, svc)

	// 用全新数据库模拟“清空后重新导入”
	db2, err := store.Open(filepath.Join(t.TempDir(), "replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Sql.Close()
	if err := engine.ImportForTest(db2, bundle); err != nil {
		t.Fatal(err)
	}
	svc2 := engine.NewService(db2)
	stAfter := state(t, svc2)

	if len(stBefore.Events) != len(stAfter.Events) {
		t.Fatalf("事件数不一致: %d vs %d", len(stBefore.Events), len(stAfter.Events))
	}
	revB := map[string]int{}
	for _, ev := range stBefore.Events {
		revB[ev.ID] = ev.Revision
	}
	for _, ev := range stAfter.Events {
		if revB[ev.ID] != ev.Revision {
			t.Fatalf("事件修订未随运行记录恢复: %s %d vs %d", ev.ID, revB[ev.ID], ev.Revision)
		}
	}
	if len(stBefore.Statuses) != len(stAfter.Statuses) {
		t.Fatalf("状态数不一致")
	}
	for i := range stBefore.Statuses {
		if stBefore.Statuses[i].State != stAfter.Statuses[i].State {
			t.Fatalf("状态重放不一致: %+v vs %+v", stBefore.Statuses[i], stAfter.Statuses[i])
		}
	}
}

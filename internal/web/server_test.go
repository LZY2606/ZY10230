package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"fossilcourt/internal/engine"
	"fossilcourt/internal/fixture"
	"fossilcourt/internal/store"
	"fossilcourt/internal/web"
)

func setup(t *testing.T) (*httptest.Server, *engine.Service) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Sql.Close() })
	svc := engine.NewService(db)
	if err := svc.SeedData(fixture.Fixed()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(web.NewServer(db, svc).Handler())
	t.Cleanup(srv.Close)
	return srv, svc
}

func TestIndexShowsTitle(t *testing.T) {
	srv, _ := setup(t)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 4096)
	n, _ := res.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "化石带议庭") {
		t.Fatalf("首页缺少标题“化石带议庭”")
	}
}

func TestStateAPIDistinguishesStates(t *testing.T) {
	srv, _ := setup(t)
	res, err := http.Get(srv.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var st engine.State
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if len(st.Sections) != 3 || len(st.Events) == 0 {
		t.Fatalf("状态不完整: sections=%d events=%d", len(st.Sections), len(st.Events))
	}
}

func TestSVG(t *testing.T) {
	srv, _ := setup(t)
	res, err := http.Get(srv.URL + "/svg/sections")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
		t.Fatalf("非 SVG 响应: %s", ct)
	}
}

func TestAddTieCycleConflictResponse(t *testing.T) {
	srv, _ := setup(t)
	body := map[string]string{
		"aGroup": "~U", "aSection": "S1", "aKind": "FAD",
		"bGroup": "~U", "bSection": "S3", "bKind": "LAD", "note": "闭合",
	}
	raw, _ := json.Marshal(body)
	res, err := http.Post(srv.URL+"/api/ties", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("期望 409, got %d", res.StatusCode)
	}
	var resp map[string]any
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["conflict"] != true {
		t.Fatalf("期望 conflict=true, got %v", resp["conflict"])
	}
	chain, _ := resp["conflictChain"].([]any)
	if len(chain) < 3 {
		t.Fatalf("冲突链过短: %d", len(chain))
	}
}

func TestResetReplay(t *testing.T) {
	srv, _ := setup(t)
	res, err := http.Post(srv.URL+"/api/reset", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("reset 状态码 %d", res.StatusCode)
	}
}

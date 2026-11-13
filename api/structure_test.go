package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func testStructureLifecycle(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	a := &authService{store: &fakeStore{u: user{Role: "admin"}}}
	mux := http.NewServeMux()
	registerStructure(mux, a, pool)
	call := func(method, path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/admin/"+path, bytes.NewReader(b))
		r.AddCookie(&http.Cookie{Name: "lms_session", Value: strings.Repeat("A", 43)})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	expect := func(status int, method, path string, body any) map[string]any {
		t.Helper()
		code, out := call(method, path, body)
		if code != status {
			t.Fatalf("%s %s: %d %#v", method, path, code, out)
		}
		return out
	}
	org := expect(201, "POST", "orgs", map[string]any{"name": "Историческая организация"})
	orgID := org["id"].(string)
	first := expect(201, "POST", "subdivisions", map[string]any{"name": "Отдел А", "org_id": orgID})
	second := expect(201, "POST", "subdivisions", map[string]any{"name": "Отдел Б", "org_id": orgID})
	sid := first["id"].(string)
	// Два конкурентных перемещения не создают цикл.
	result := make(chan int, 2)
	for _, pair := range [][2]map[string]any{{first, second}, {second, first}} {
		go func(pair [2]map[string]any) {
			s, p := pair[0], pair[1]
			code, _ := call("PUT", "subdivisions/"+s["id"].(string), map[string]any{"name": s["name"], "org_id": orgID, "parent_id": p["id"], "updated_at": s["updated_at"]})
			result <- code
		}(pair)
	}
	x, y := <-result, <-result
	if !(x == 200 && y == 409 || y == 200 && x == 409) {
		t.Fatal("cycle", x, y)
	}
	p := expect(201, "POST", "positions", map[string]any{"name": "Разработчик", "subdivision_id": sid})
	pid := p["id"].(string)
	ids := []string{}
	for _, name := range []string{"Первый", "Второй"} {
		var id string
		if e := pool.QueryRow(ctx, "INSERT INTO collaborators(lastname,hire_date) VALUES($1,'2025-01-01') RETURNING id::text", name).Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	// Два сотрудника конкурируют за одну вакансию.
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			code, _ := call("POST", "positions/"+pid+"/assign", map[string]any{"collaborator_id": id, "updated_at": p["updated_at"], "date": "2020-01-02"})
			result <- code
		}(id)
	}
	wg.Wait()
	x, y = <-result, <-result
	if !(x == 200 && y == 409 || y == 200 && x == 409) {
		t.Fatal("vacancy", x, y)
	}
	p = expect(200, "GET", "positions/"+pid, nil)
	owner := p["collaborator_id"].(string)
	state := expect(200, "GET", "collaborators/"+owner+"/positions", nil)
	employee := state["collaborator"].(map[string]any)
	if employee["main_position_id"] != pid {
		t.Fatal("auto main", state)
	}
	var assignedDate string
	if e := pool.QueryRow(ctx, "SELECT date::text FROM collaborator_position_history WHERE collaborator_id=$1 AND event_type='assigned'", owner).Scan(&assignedDate); e != nil || assignedDate != "2020-01-02" {
		t.Fatal(assignedDate, e)
	}
	// Переименование не меняет исторические снимки.
	expect(200, "PUT", "orgs/"+orgID, map[string]any{"name": "Новое имя", "updated_at": org["updated_at"]})
	first = expect(200, "GET", "subdivisions/"+sid, nil)
	expect(200, "PUT", "subdivisions/"+sid, map[string]any{"name": "Новый отдел", "org_id": orgID, "parent_id": first["parent_id"], "updated_at": first["updated_at"]})
	p = expect(200, "PUT", "positions/"+pid, map[string]any{"name": "Новая должность", "subdivision_id": sid, "updated_at": p["updated_at"]})
	var oldOrg, oldSub, oldPosition string
	if e := pool.QueryRow(ctx, "SELECT org_name,subdivision_name,position_name FROM collaborator_position_history WHERE collaborator_id=$1 AND event_type='assigned'", owner).Scan(&oldOrg, &oldSub, &oldPosition); e != nil || oldOrg != "Историческая организация" || oldSub != "Отдел А" || oldPosition != "Разработчик" {
		t.Fatal(oldOrg, oldSub, oldPosition, e)
	}
	p2 := expect(201, "POST", "positions", map[string]any{"name": "Вторая", "subdivision_id": sid})
	id2 := p2["id"].(string)
	expect(200, "POST", "positions/"+id2+"/assign", map[string]any{"collaborator_id": owner, "updated_at": p2["updated_at"]})
	state = expect(200, "GET", "collaborators/"+owner+"/positions", nil)
	employee = state["collaborator"].(map[string]any)
	state = expect(200, "PUT", "collaborators/"+owner+"/main-position", map[string]any{"position_id": id2, "updated_at": employee["updated_at"]})
	employee = state["collaborator"].(map[string]any)
	if employee["main_position_id"] != id2 {
		t.Fatal(state)
	}
	p2 = expect(200, "GET", "positions/"+id2, nil)
	state = expect(200, "POST", "positions/"+id2+"/unassign", map[string]any{"updated_at": p2["updated_at"], "date": "2020-02-01"})
	employee = state["collaborator"].(map[string]any)
	if employee["main_position_id"] != nil {
		t.Fatal("stale main response", state)
	}
	history := expect(200, "GET", "collaborators/"+owner+"/position-history", nil)
	events := history["items"].([]any)
	for i := 1; i < len(events); i++ {
		if events[i-1].(map[string]any)["event_seq"].(float64) <= events[i].(map[string]any)["event_seq"].(float64) {
			t.Fatal("event order")
		}
	}
	if len(events) != 7 {
		t.Fatal("unexpected history event count", len(events))
	}
	var recordedLater bool
	if e := pool.QueryRow(ctx, "SELECT created_at::date <> date FROM collaborator_position_history WHERE collaborator_id=$1 AND event_type='assigned' ORDER BY event_seq LIMIT 1", owner).Scan(&recordedLater); e != nil || !recordedLater {
		t.Fatal("HR date vs registration time", e)
	}
	p = expect(200, "GET", "positions/"+pid, nil)
	closed := expect(200, "PUT", "positions/"+pid, map[string]any{"name": p["name"], "subdivision_id": sid, "status": "close", "updated_at": p["updated_at"]})
	if closed["collaborator_id"] != owner {
		t.Fatal("closing detached employee", closed)
	}
	expect(409, "PUT", "positions/"+pid, map[string]any{"name": "Stale", "subdivision_id": sid, "updated_at": p["updated_at"]})
	// Физическое удаление должностей запрещено: тестовая БД целиком удаляется fixture.
}

func TestStructurePermissions(t *testing.T) {
	a := &authService{store: &fakeStore{u: user{Role: "employee"}}}
	mux := http.NewServeMux()
	registerStructure(mux, a, nil)
	for _, route := range []struct{ method, path string }{{"GET", "orgs"}, {"POST", "orgs"}, {"PUT", "orgs/x"}, {"GET", "orgs/x"}, {"GET", "subdivisions"}, {"POST", "subdivisions"}, {"PUT", "subdivisions/x"}, {"GET", "positions"}, {"POST", "positions"}, {"PUT", "positions/x"}, {"POST", "positions/x/assign"}, {"POST", "positions/x/unassign"}, {"GET", "collaborators/x/positions"}, {"GET", "collaborators/x/position-history"}, {"PUT", "collaborators/x/main-position"}} {
		r := httptest.NewRequest(route.method, "/admin/"+route.path, nil)
		r.AddCookie(&http.Cookie{Name: "lms_session", Value: strings.Repeat("A", 43)})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(route, w.Code)
		}
	}
}

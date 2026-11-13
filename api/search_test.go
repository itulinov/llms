package main

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testFullCardSearch(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var org, sub, cid, pid string
	must := func(e error) {
		if e != nil {
			t.Fatal(e)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO orgs(name,data) VALUES('SEARCHORG','{"hidden_key_needle":{"array":["deep_value_needle"]}}') RETURNING id::text`).Scan(&org))
	must(pool.QueryRow(ctx, `INSERT INTO subdivisions(name,org_id,status) VALUES('SEARCHSUB',$1,'project') RETURNING id::text`, org).Scan(&sub))
	must(pool.QueryRow(ctx, `INSERT INTO collaborators(lastname,login,birth_date,data) VALUES('SEARCHPERSON','cadre_needle','2000-02-01','{"nested":{"list":["person_extra_needle"]}}') RETURNING id::text`).Scan(&cid))
	must(pool.QueryRow(ctx, `INSERT INTO accounts(login,password_hash,role,collaborator_id,is_active) VALUES('account_needle','secret_hash_needle','employee',$1,false) RETURNING id::text`, cid).Scan(new(string)))
	must(pool.QueryRow(ctx, `INSERT INTO positions(name,subdivision_id,collaborator_id,is_boss,data) VALUES('SEARCHPOSITION',$1,$2,true,'{"nested":["position_extra_needle"]}') RETURNING id::text`, sub, cid).Scan(&pid))
	mux := http.NewServeMux()
	a := &authService{store: &fakeStore{u: user{Role: "admin"}}}
	registerEmployees(mux, a, pool)
	registerStructure(mux, a, pool)
	for _, tc := range []struct {
		entity, q string
		count     int
	}{{"orgs", "deep_value_needle", 1}, {"orgs", "hidden_key_needle", 0}, {"collaborators", "person_extra_needle", 1}, {"collaborators", "cadre_needle", 1}, {"collaborators", "account_needle", 1}, {"collaborators", "Заблокирована", 1}, {"collaborators", "01.02.2000", 1}, {"collaborators", "searchsub", 1}, {"collaborators", "secret_hash_needle", 0}, {"positions", "position_extra_needle", 1}, {"positions", "SEARCHPERSON", 1}, {"positions", "SEARCHORG", 1}, {"subdivisions", "SEARCHORG", 1}, {"subdivisions", "Проект", 1}} {
		r := httptest.NewRequest("GET", "/admin/"+tc.entity+"?q="+url.QueryEscape(tc.q), nil)
		r.AddCookie(&http.Cookie{Name: "lms_session", Value: strings.Repeat("A", 43)})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var out struct {
			Total int `json:"total"`
		}
		e := json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 200 || e != nil || out.Total != tc.count {
			t.Fatalf("%s %s: HTTP %d %s", tc.entity, tc.q, w.Code, w.Body.String())
		}
	}
}

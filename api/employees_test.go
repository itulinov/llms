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
	"time"
)

func TestEmployeeValidation(t *testing.T) {
	for _, e := range []employeeInput{{Lastname: ""}, {Lastname: "Иванов", Email: ptr("invalid")}, {Lastname: "Иванов", HireDate: ptr("2026-02-30")}, {Lastname: "Иванов", HireDate: ptr("2026-11-10"), DismissDate: ptr("2026-10-01")}} {
		if e.validate() {
			t.Fatal(e)
		}
	}
	e := employeeInput{Lastname: " Иванов ", Firstname: ptr(" ")}
	if !e.validate() || e.Lastname != "Иванов" || e.Firstname != nil {
		t.Fatal(e)
	}
}
func ptr(s string) *string { return &s }
func testEmployeeLifecycle(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	a := &authService{store: &fakeStore{u: user{Role: "admin"}}}
	mux := http.NewServeMux()
	registerEmployees(mux, a, pool)
	call := func(method, path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.AddCookie(&http.Cookie{Name: "lms_session", Value: strings.Repeat("A", 43)})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	input := map[string]any{"lastname": "Ёлкин_%", "email": "test@example.org", "is_dismiss": false}
	code, out := call("POST", "/admin/collaborators", input)
	if code != 201 {
		t.Fatal(code, out)
	}
	id := out["id"].(string)
	path := "/admin/collaborators/" + id
	defer pool.Exec(ctx, "DELETE FROM collaborators WHERE id=$1", id)
	defer pool.Exec(ctx, "DELETE FROM accounts WHERE collaborator_id=$1", id)
	code, out = call("GET", path, nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	version := out["updated_at"]
	code, out = call("POST", path+"/account", map[string]any{"login": "lifecycle", "password": "valid-password-123"})
	if code != 200 {
		t.Fatal(code, out)
	}
	code, _ = call("POST", path+"/account", map[string]any{"login": "lifecycle2", "password": "valid-password-123"})
	if code != 409 {
		t.Fatal("duplicate", code)
	}
	var account string
	pool.QueryRow(ctx, "SELECT id::text FROM accounts WHERE collaborator_id=$1", id).Scan(&account)
	store := pgAuthStore{pool}
	token := make([]byte, 32)
	if err := store.create(ctx, account, token, time.Hour); err != nil {
		t.Fatal(err)
	}
	code, _ = call("PUT", path+"/account/status", map[string]any{"is_active": false})
	if code != 200 {
		t.Fatal(code)
	}
	var n int
	pool.QueryRow(ctx, "SELECT count(*) FROM auth_sessions WHERE account_id=$1", account).Scan(&n)
	if n != 0 {
		t.Fatal("sessions survived", n)
	}
	input["updated_at"] = version
	input["is_dismiss"] = true
	code, _ = call("PUT", path, input)
	if code != 200 {
		t.Fatal("dismiss", code)
	}
	code, _ = call("PUT", path, input)
	if code != 409 {
		t.Fatal("stale version", code)
	}
	_, out = call("GET", path, nil)
	input["updated_at"] = out["updated_at"]
	input["is_dismiss"] = false
	code, _ = call("PUT", path, input)
	if code != 200 {
		t.Fatal("restore", code)
	}
	var active bool
	pool.QueryRow(ctx, "SELECT is_active FROM accounts WHERE id=$1", account).Scan(&active)
	if !active {
		t.Fatal("manual block not removed")
	}
	code, out = call("GET", "/admin/collaborators?q=%25&status=all&limit=25", nil)
	if code != 200 || out["total"].(float64) != 1 {
		t.Fatal("literal search", code, out)
	}
	// Ровно один из двух редакторов с одной версией может сохранить карточку.
	_, out = call("GET", path, nil)
	input["updated_at"] = out["updated_at"]
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c, _ := call("PUT", path, input); codes <- c }()
	}
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for c := range codes {
		if c == 200 {
			ok++
		}
		if c == 409 {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal("concurrent edits", ok, conflict)
	}
	// Два увольнения разных admin сериализованы общим advisory lock.
	adminIDs := []string{}
	for _, name := range []string{"AdminOne", "AdminTwo"} {
		var cid string
		if err := pool.QueryRow(ctx, "INSERT INTO collaborators(lastname) VALUES($1) RETURNING id::text", name).Scan(&cid); err != nil {
			t.Fatal(err)
		}
		adminIDs = append(adminIDs, cid)
		if _, err := pool.Exec(ctx, "INSERT INTO accounts(login,password_hash,role,collaborator_id) VALUES($1,'test','admin',$2)", name, cid); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, "DELETE FROM collaborators WHERE id=$1", cid)
		defer pool.Exec(ctx, "DELETE FROM accounts WHERE collaborator_id=$1", cid)
	}
	adminCodes := make(chan int, 2)
	for _, cid := range adminIDs {
		_, details := call("GET", "/admin/collaborators/"+cid, nil)
		go func(cid string, version any) {
			c, _ := call("PUT", "/admin/collaborators/"+cid, map[string]any{"lastname": "Admin", "is_dismiss": true, "updated_at": version})
			adminCodes <- c
		}(cid, details["updated_at"])
	}
	first, second := <-adminCodes, <-adminCodes
	if !(first == 200 && second == 409 || first == 409 && second == 200) {
		t.Fatal("last admin protection", first, second)
	}
	// Вход ожидает блокировки accounts и затем видит неактивность.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", account); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- store.create(ctx, account, make([]byte, 32), time.Hour) }()
	if _, err = tx.Exec(ctx, "UPDATE accounts SET is_active=false WHERE id=$1", account); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err == nil {
		t.Fatal("session created after block")
	}

}

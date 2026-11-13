package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

const adminLock int64 = 2026111301

type employeeInput struct {
	Code        *string `json:"code"`
	Lastname    string  `json:"lastname"`
	Firstname   *string `json:"firstname"`
	Middlename  *string `json:"middlename"`
	Email       *string `json:"email"`
	HireDate    *string `json:"hire_date"`
	DismissDate *string `json:"dismiss_date"`
	IsDismiss   bool    `json:"is_dismiss"`
	UpdatedAt   string  `json:"updated_at"`
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid JSON")
	}
	return nil
}
func (e *employeeInput) validate() bool {
	e.Lastname = strings.TrimSpace(e.Lastname)
	if e.Lastname == "" || len(e.Lastname) > 512 {
		return false
	}
	for _, p := range []**string{&e.Code, &e.Firstname, &e.Middlename, &e.Email, &e.HireDate, &e.DismissDate} {
		if *p != nil {
			s := strings.TrimSpace(**p)
			if s == "" {
				*p = nil
			} else {
				*p = &s
			}
		}
	}
	if e.Email != nil {
		m, err := mail.ParseAddress(*e.Email)
		if err != nil || m.Address != *e.Email {
			return false
		}
	}
	for _, d := range []*string{e.HireDate, e.DismissDate} {
		if d != nil {
			if _, err := time.Parse("2006-01-02", *d); err != nil {
				return false
			}
		}
	}
	return e.HireDate == nil || e.DismissDate == nil || *e.HireDate <= *e.DismissDate
}
func dbFail(w http.ResponseWriter, e error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(e, pgx.ErrNoRows):
		fail(w, 404)
	case errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "23503"):
		fail(w, 409)
	case errors.As(e, &pg) && pg.Code == "22P02":
		fail(w, 400)
	default:
		fail(w, 503)
	}
}

const employeeJSON = `jsonb_build_object('id',c.id,'code',c.code,'lastname',c.lastname,'firstname',c.firstname,'middlename',c.middlename,'fullname',c.fullname,'email',c.email,'hire_date',c.hire_date,'dismiss_date',c.dismiss_date,'is_dismiss',c.is_dismiss,'updated_at',to_char(c.updated_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'position_name',v.position_name,'subdivision_name',v.position_parent_name,'org_name',v.org_name,'account',(SELECT jsonb_build_object('id',a.id,'login',a.login,'role',a.role,'is_active',a.is_active) FROM accounts a WHERE a.collaborator_id=c.id))`

func registerEmployees(mux *http.ServeMux, a *authService, pool *pgxpool.Pool) {
	handle := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.RequireAuth(RequireRole("admin", h))) }
	handle("GET /admin/collaborators", func(w http.ResponseWriter, r *http.Request) {
		limit, offset := 25, 0
		var err error
		if s := r.URL.Query().Get("limit"); s != "" {
			limit, err = strconv.Atoi(s)
		}
		if err != nil || limit < 1 || limit > 100 {
			fail(w, 400)
			return
		}
		if s := r.URL.Query().Get("offset"); s != "" {
			offset, err = strconv.Atoi(s)
		}
		if err != nil || offset < 0 {
			fail(w, 400)
			return
		}
		status := r.URL.Query().Get("status")
		if status != "" && status != "all" && status != "active" && status != "dismissed" {
			fail(w, 400)
			return
		}
		q := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(r.URL.Query().Get("q")))
		tx, e := pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			dbFail(w, e)
			return
		}
		defer tx.Rollback(r.Context())
		where := ` WHERE ($1='' OR c.fullname ILIKE $1 ESCAPE '\' OR c.code ILIKE $1 ESCAPE '\' OR c.email ILIKE $1 ESCAPE '\') AND ($2 IN ('','all') OR c.is_dismiss=($2='dismissed'))`
		if q != "" {
			q = "%" + q + "%"
		}
		var total int
		if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM collaborators c"+where, q, status).Scan(&total); e != nil {
			dbFail(w, e)
			return
		}
		rows, e := tx.Query(r.Context(), "SELECT "+employeeJSON+" FROM collaborators c LEFT JOIN v_collaborators v ON v.id=c.id"+where+" ORDER BY c.fullname,c.id LIMIT $3 OFFSET $4", q, status, limit, offset)
		if e != nil {
			dbFail(w, e)
			return
		}
		items := []json.RawMessage{}
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				break
			}
			items = append(items, json.RawMessage(b))
		}
		rows.Close()
		if e == nil {
			e = rows.Err()
		}
		if e == nil {
			e = tx.Commit(r.Context())
		}
		if e != nil {
			dbFail(w, e)
			return
		}
		reply(w, 200, map[string]any{"items": items, "total": total})
	})
	handle("GET /admin/collaborators/{id}", func(w http.ResponseWriter, r *http.Request) {
		var b []byte
		e := pool.QueryRow(r.Context(), "SELECT "+employeeJSON+" FROM collaborators c LEFT JOIN v_collaborators v ON v.id=c.id WHERE c.id=$1", r.PathValue("id")).Scan(&b)
		if e != nil {
			dbFail(w, e)
			return
		}
		reply(w, 200, json.RawMessage(b))
	})
	for _, method := range []string{"POST", "PUT"} {
		handle(method+" /admin/collaborators"+map[bool]string{true: "/{id}"}[method == "PUT"], func(w http.ResponseWriter, r *http.Request) {
			var input employeeInput
			if decode(r, &input) != nil || !input.validate() {
				fail(w, 400)
				return
			}
			ctx := r.Context()
			tx, e := pool.Begin(ctx)
			if e != nil {
				dbFail(w, e)
				return
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", adminLock); e != nil {
				dbFail(w, e)
				return
			}
			id := r.PathValue("id")
			if r.Method == "POST" {
				e = tx.QueryRow(ctx, `INSERT INTO collaborators(code,lastname,firstname,middlename,email,hire_date,dismiss_date,is_dismiss) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, input.Code, input.Lastname, input.Firstname, input.Middlename, input.Email, input.HireDate, input.DismissDate, input.IsDismiss).Scan(&id)
			} else {
				var old bool
				var updated time.Time
				e = tx.QueryRow(ctx, "SELECT is_dismiss,updated_at FROM collaborators WHERE id=$1 FOR UPDATE", id).Scan(&old, &updated)
				if e != nil {
					dbFail(w, e)
					return
				}
				version, err := time.Parse(time.RFC3339Nano, input.UpdatedAt)
				if err != nil {
					fail(w, 400)
					return
				}
				if !version.Equal(updated) {
					fail(w, 409)
					return
				}
				if old != input.IsDismiss {
					if e = transitionAccess(ctx, tx, id, !input.IsDismiss); e != nil {
						if errors.Is(e, errConflict) {
							fail(w, 409)
						} else {
							dbFail(w, e)
						}
						return
					}
				}
				_, e = tx.Exec(ctx, `UPDATE collaborators SET code=$2,lastname=$3,firstname=$4,middlename=$5,email=$6,hire_date=$7,dismiss_date=$8,is_dismiss=$9 WHERE id=$1`, id, input.Code, input.Lastname, input.Firstname, input.Middlename, input.Email, input.HireDate, input.DismissDate, input.IsDismiss)
			}
			if e == nil {
				e = tx.Commit(ctx)
			}
			if e != nil {
				dbFail(w, e)
				return
			}
			reply(w, map[bool]int{true: 201, false: 200}[r.Method == "POST"], map[string]string{"id": id})
		})
	}
	registerAccountManagement(handle, pool)
}

var errConflict = errors.New("conflict")

func transitionAccess(ctx context.Context, tx pgx.Tx, collaborator string, active bool) error {
	var id, role string
	var was bool
	e := tx.QueryRow(ctx, "SELECT id::text,role,is_active FROM accounts WHERE collaborator_id=$1 FOR UPDATE", collaborator).Scan(&id, &role, &was)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if role == "admin" && was && !active {
		var n int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM accounts WHERE role='admin' AND is_active").Scan(&n); e != nil {
			return e
		}
		if n <= 1 {
			return errConflict
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE accounts SET is_active=$2 WHERE id=$1", id, active); e != nil {
		return e
	}
	if !active {
		_, e = tx.Exec(ctx, "DELETE FROM auth_sessions WHERE account_id=$1", id)
	}
	return e
}
func registerAccountManagement(handle func(string, http.HandlerFunc), pool *pgxpool.Pool) {
	for _, action := range []string{"create", "status", "password"} {
		suffix, method := "/account", "POST"
		if action == "status" {
			suffix += "/status"
			method = "PUT"
		}
		if action == "password" {
			suffix += "/password"
		}
		action := action
		handle(method+" /admin/collaborators/{id}"+suffix, func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Login    string `json:"login"`
				Password string `json:"password"`
				IsActive *bool  `json:"is_active"`
			}
			if decode(r, &in) != nil {
				fail(w, 400)
				return
			}
			var hash string
			var e error
			if action != "status" {
				hash, e = hashPassword(in.Password)
				if e != nil {
					fail(w, 400)
					return
				}
			}
			if action == "create" {
				in.Login, e = normalizeLogin(in.Login)
				if e != nil {
					fail(w, 400)
					return
				}
			}
			ctx := r.Context()
			tx, e := pool.Begin(ctx)
			if e != nil {
				dbFail(w, e)
				return
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", adminLock); e != nil {
				dbFail(w, e)
				return
			}
			var dismissed bool
			collab := r.PathValue("id")
			if e = tx.QueryRow(ctx, "SELECT is_dismiss FROM collaborators WHERE id=$1 FOR UPDATE", collab).Scan(&dismissed); e != nil {
				dbFail(w, e)
				return
			}
			if action == "create" {
				_, e = tx.Exec(ctx, "INSERT INTO accounts(login,password_hash,role,is_active,collaborator_id) VALUES($1,$2,'employee',$3,$4)", in.Login, hash, !dismissed, collab)
			} else {
				var id, role string
				if e = tx.QueryRow(ctx, "SELECT id::text,role FROM accounts WHERE collaborator_id=$1 FOR UPDATE", collab).Scan(&id, &role); e != nil {
					dbFail(w, e)
					return
				}
				if role != "employee" {
					fail(w, 409)
					return
				}
				if action == "status" {
					if in.IsActive == nil {
						fail(w, 400)
						return
					}
					if dismissed && *in.IsActive {
						fail(w, 409)
						return
					}
					e = transitionAccess(ctx, tx, collab, *in.IsActive)
				} else {
					_, e = tx.Exec(ctx, "UPDATE accounts SET password_hash=$2 WHERE id=$1", id, hash)
					if e == nil {
						_, e = tx.Exec(ctx, "DELETE FROM auth_sessions WHERE account_id=$1", id)
					}
				}
			}
			if e == nil {
				e = tx.Commit(ctx)
			}
			if e != nil {
				dbFail(w, e)
				return
			}
			reply(w, 200, map[string]bool{"ok": true})
		})
	}
}

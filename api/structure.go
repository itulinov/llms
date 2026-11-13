package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type structureSpec struct {
	table     string
	fields    []string
	immutable string
}

var structureSpecs = []structureSpec{{"orgs", []string{"code", "name", "disp_name"}, ""}, {"subdivisions", []string{"code", "name", "org_id", "parent_id", "status", "is_disbanded", "formed_date", "disbanded_date"}, "org_id"}, {"positions", []string{"code", "name", "subdivision_id", "status", "is_boss", "start_date", "finish_date"}, "subdivision_id"}}

func structureJSON(alias string) string {
	return "(to_jsonb(" + alias + ")-'data') || jsonb_build_object('updated_at',to_char(" + alias + ".updated_at AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'))"
}
func structureRead(ctx context.Context, tx pgx.Tx, table, id string) (json.RawMessage, error) {
	var b []byte
	e := tx.QueryRow(ctx, "SELECT "+structureJSON("t")+" FROM "+table+" t WHERE id=$1", id).Scan(&b)
	return b, e
}
func pagination(r *http.Request) (int, int, error) {
	limit, offset := 25, 0
	var e error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, e = strconv.Atoi(v)
		if e != nil {
			return 0, 0, e
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, e = strconv.Atoi(v)
	}
	if e != nil || limit < 1 || limit > 100 || offset < 0 {
		return 0, 0, errConflict
	}
	return limit, offset, nil
}
func structureError(w http.ResponseWriter, e error) {
	if errors.Is(e, errConflict) {
		fail(w, 409)
	} else {
		dbFail(w, e)
	}
}
func checkVersion(raw any, actual time.Time) bool {
	s, ok := raw.(string)
	if !ok {
		return false
	}
	v, e := time.Parse(time.RFC3339Nano, s)
	return e == nil && v.Equal(actual)
}
func registerStructure(mux *http.ServeMux, a *authService, pool *pgxpool.Pool) {
	handle := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.RequireAuth(RequireRole("admin", h))) }
	for _, spec := range structureSpecs {
		spec := spec
		handle("GET /admin/"+spec.table, func(w http.ResponseWriter, r *http.Request) {
			limit, offset, e := pagination(r)
			if e != nil {
				fail(w, 400)
				return
			}
			where := "true"
			orgFilter := r.URL.Query().Get("org_id")
			subFilter := r.URL.Query().Get("subdivision_id")
			args := []any{}
			if spec.immutable != "" && r.URL.Query().Get(spec.immutable) != "" {
				args = append(args, r.URL.Query().Get(spec.immutable))
				where = spec.immutable + "=$1"
			}
			if spec.table == "positions" {
				if v := r.URL.Query().Get("occupancy"); v != "" && v != "all" {
					if v != "vacant" && v != "occupied" {
						fail(w, 400)
						return
					}
					if v == "vacant" {
						where += " AND collaborator_id IS NULL"
					} else {
						where += " AND collaborator_id IS NOT NULL"
					}
				}
				if v := r.URL.Query().Get("status"); v != "" {
					args = append(args, v)
					where += " AND status=$" + strconv.Itoa(len(args))
				}
			}
			if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
				q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
				args = append(args, "%"+q+"%")
				param := "$" + strconv.Itoa(len(args))
				where += " AND (name ILIKE " + param + " OR code ILIKE " + param + ")"
			}
			if spec.table == "subdivisions" {
				if v := r.URL.Query().Get("status"); v != "" && v != "all" {
					args = append(args, v)
					where += " AND status=$" + strconv.Itoa(len(args))
				}
			}
			if spec.table == "positions" && orgFilter != "" {
				args = append(args, orgFilter)
				where += " AND subdivision_id IN (SELECT id FROM subdivisions WHERE org_id=$" + strconv.Itoa(len(args)) + ")"
			}
			tx, e := pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if e != nil {
				dbFail(w, e)
				return
			}
			defer tx.Rollback(r.Context())
			if spec.table == "positions" && orgFilter != "" && subFilter != "" {
				var belongs bool
				e = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT FROM subdivisions WHERE id=$1 AND org_id=$2)", subFilter, orgFilter).Scan(&belongs)
				if e != nil {
					dbFail(w, e)
					return
				}
				if !belongs {
					fail(w, 400)
					return
				}
			}
			var total int
			e = tx.QueryRow(r.Context(), "SELECT count(*) FROM "+spec.table+" WHERE "+where, args...).Scan(&total)
			if e != nil {
				dbFail(w, e)
				return
			}
			args = append(args, limit, offset)
			rows, e := tx.Query(r.Context(), "SELECT "+structureListJSON(spec.table)+" FROM "+spec.table+" t WHERE "+where+" ORDER BY name,id LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
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
				items = append(items, b)
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
		handle("GET /admin/"+spec.table+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			tx, e := pool.Begin(r.Context())
			if e != nil {
				dbFail(w, e)
				return
			}
			defer tx.Rollback(r.Context())
			out, e := structureRead(r.Context(), tx, spec.table, r.PathValue("id"))
			if e != nil {
				dbFail(w, e)
				return
			}
			reply(w, 200, out)
		})
		for _, method := range []string{"POST", "PUT"} {
			suffix := ""
			if method == "PUT" {
				suffix = "/{id}"
			}
			handle(method+" /admin/"+spec.table+suffix, func(w http.ResponseWriter, r *http.Request) {
				var input map[string]any
				if decode(r, &input) != nil {
					fail(w, 400)
					return
				}
				allowed := map[string]bool{"updated_at": true}
				for _, f := range spec.fields {
					allowed[f] = true
				}
				for k := range input {
					if !allowed[k] {
						fail(w, 400)
						return
					}
				}
				name, ok := input["name"].(string)
				if !ok || strings.TrimSpace(name) == "" {
					fail(w, 400)
					return
				}
				input["name"] = strings.TrimSpace(name)
				for _, f := range spec.fields {
					if strings.HasSuffix(f, "_id") && input[f] != nil {
						value, ok := input[f].(string)
						var uuid pgtype.UUID
						if !ok || uuid.Scan(value) != nil || !uuid.Valid {
							fail(w, 400)
							return
						}
					}
				}
				for k, v := range input {
					if strings.HasSuffix(k, "_date") && v != nil && v != "" {
						s, ok := v.(string)
						if !ok {
							fail(w, 400)
							return
						}
						if _, e := time.Parse("2006-01-02", s); e != nil {
							fail(w, 400)
							return
						}
					}
					if v == "" {
						input[k] = nil
					}
				}
				if spec.immutable != "" {
					value, ok := input[spec.immutable].(string)
					if !ok || value == "" {
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
				id := r.PathValue("id")
				if id != "" {
					var version time.Time
					e = tx.QueryRow(ctx, "SELECT updated_at FROM "+spec.table+" WHERE id=$1 FOR UPDATE", id).Scan(&version)
					if e != nil {
						dbFail(w, e)
						return
					}
					if !checkVersion(input["updated_at"], version) {
						fail(w, 409)
						return
					}
					if spec.immutable != "" {
						var old string
						e = tx.QueryRow(ctx, "SELECT "+spec.immutable+"::text FROM "+spec.table+" WHERE id=$1", id).Scan(&old)
						if e != nil {
							dbFail(w, e)
							return
						}
						if input[spec.immutable] != old {
							fail(w, 409)
							return
						}
					}
				}
				if spec.table == "subdivisions" && input["parent_id"] != nil {
					var org string
					e = tx.QueryRow(ctx, "SELECT org_id::text FROM subdivisions WHERE id=$1", input["parent_id"]).Scan(&org)
					if e != nil {
						dbFail(w, e)
						return
					}
					if input["org_id"] != org {
						fail(w, 409)
						return
					}
					if id != "" {
						var cycle bool
						e = tx.QueryRow(ctx, `WITH RECURSIVE ancestors AS (SELECT id,parent_id FROM subdivisions WHERE id=$1 UNION SELECT s.id,s.parent_id FROM subdivisions s JOIN ancestors p ON s.id=p.parent_id) SELECT EXISTS(SELECT FROM ancestors WHERE id=$2)`, input["parent_id"], id).Scan(&cycle)
						if e != nil {
							dbFail(w, e)
							return
						}
						if cycle {
							fail(w, 409)
							return
						}
					}
				}
				columns, values, sets := []string{}, []string{}, []string{}
				args := []any{}
				for _, f := range spec.fields {
					v, exists := input[f]
					if !exists {
						continue
					}
					if f == "status" {
						s, ok := v.(string)
						if !ok || !strings.Contains("|active|plan|project|close|cancel|", "|"+s+"|") {
							fail(w, 400)
							return
						}
					}
					if f == "is_boss" || f == "is_disbanded" {
						if _, ok := v.(bool); !ok {
							fail(w, 400)
							return
						}
					}
					args = append(args, v)
					columns = append(columns, f)
					param := "$" + strconv.Itoa(len(args))
					values = append(values, param)
					sets = append(sets, f+"="+param)
				}
				if id == "" {
					e = tx.QueryRow(ctx, "INSERT INTO "+spec.table+"("+strings.Join(columns, ",")+") VALUES("+strings.Join(values, ",")+") RETURNING id::text", args...).Scan(&id)
				} else {
					args = append(args, id)
					_, e = tx.Exec(ctx, "UPDATE "+spec.table+" SET "+strings.Join(sets, ",")+" WHERE id=$"+strconv.Itoa(len(args)), args...)
				}
				if e != nil {
					structureError(w, e)
					return
				}
				out, e := structureRead(ctx, tx, spec.table, id)
				if e == nil {
					e = tx.Commit(ctx)
				}
				if e != nil {
					dbFail(w, e)
					return
				}
				status := 200
				if r.Method == "POST" {
					status = 201
				}
				reply(w, status, out)
			})
		}
	}
	registerAssignments(handle, pool)
}

func structureListJSON(table string) string {
	base := structureJSON("t")
	switch table {
	case "subdivisions":
		return base + ` || jsonb_build_object('org_name',(SELECT name FROM orgs WHERE id=t.org_id),'parent_name',(SELECT name FROM subdivisions WHERE id=t.parent_id))`
	case "positions":
		return base + ` || jsonb_build_object('subdivision_name',(SELECT name FROM subdivisions WHERE id=t.subdivision_id),'org_id',(SELECT org_id FROM subdivisions WHERE id=t.subdivision_id),'org_name',(SELECT o.name FROM orgs o JOIN subdivisions s ON s.org_id=o.id WHERE s.id=t.subdivision_id),'collaborator_fullname',(SELECT fullname FROM collaborators WHERE id=t.collaborator_id))`
	}
	return base
}

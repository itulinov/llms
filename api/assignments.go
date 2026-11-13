package main

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"time"
)

func assignmentState(ctx context.Context, tx pgx.Tx, id string) (map[string]any, error) {
	var employee []byte
	e := tx.QueryRow(ctx, "SELECT "+structureJSON("c")+" FROM collaborators c WHERE id=$1", id).Scan(&employee)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, "SELECT "+structureJSON("p")+" FROM positions p WHERE collaborator_id=$1 ORDER BY p.id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		items = append(items, b)
	}
	return map[string]any{"collaborator": json.RawMessage(employee), "positions": items}, rows.Err()
}
func registerAssignments(handle func(string, http.HandlerFunc), pool *pgxpool.Pool) {
	handle("GET /admin/collaborators/{id}/positions", func(w http.ResponseWriter, r *http.Request) {
		tx, e := pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			dbFail(w, e)
			return
		}
		defer tx.Rollback(r.Context())
		out, e := assignmentState(r.Context(), tx, r.PathValue("id"))
		if e != nil {
			dbFail(w, e)
			return
		}
		reply(w, 200, out)
	})
	handle("GET /admin/collaborators/{id}/position-history", func(w http.ResponseWriter, r *http.Request) {
		limit, offset, e := pagination(r)
		if e != nil {
			fail(w, 400)
			return
		}
		tx, e := pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if e != nil {
			dbFail(w, e)
			return
		}
		defer tx.Rollback(r.Context())
		var exists bool
		if e = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT FROM collaborators WHERE id=$1)", r.PathValue("id")).Scan(&exists); e != nil {
			dbFail(w, e)
			return
		}
		if !exists {
			fail(w, 404)
			return
		}
		var total int
		e = tx.QueryRow(r.Context(), "SELECT count(*) FROM collaborator_position_history WHERE collaborator_id=$1", r.PathValue("id")).Scan(&total)
		if e != nil {
			dbFail(w, e)
			return
		}
		rows, e := tx.Query(r.Context(), "SELECT to_jsonb(h) FROM collaborator_position_history h WHERE collaborator_id=$1 ORDER BY event_seq DESC LIMIT $2 OFFSET $3", r.PathValue("id"), limit, offset)
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
	for _, action := range []string{"assign", "unassign"} {
		action := action
		handle("POST /admin/positions/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				CollaboratorID string  `json:"collaborator_id"`
				UpdatedAt      string  `json:"updated_at"`
				Date           *string `json:"date"`
			}
			if decode(r, &in) != nil {
				fail(w, 400)
				return
			}
			if in.Date != nil {
				if _, e := time.Parse("2006-01-02", *in.Date); e != nil {
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
			pid := r.PathValue("id")
			var current *string
			e = tx.QueryRow(ctx, "SELECT collaborator_id::text FROM positions WHERE id=$1", pid).Scan(&current)
			if e != nil {
				dbFail(w, e)
				return
			}
			cid := in.CollaboratorID
			if action == "unassign" {
				if current == nil {
					fail(w, 409)
					return
				}
				cid = *current
			}
			var dismissed bool
			if e = tx.QueryRow(ctx, "SELECT is_dismiss FROM collaborators WHERE id=$1 FOR UPDATE", cid).Scan(&dismissed); e != nil {
				dbFail(w, e)
				return
			}
			var version time.Time
			var status string
			e = tx.QueryRow(ctx, "SELECT updated_at,status,collaborator_id::text FROM positions WHERE id=$1 FOR UPDATE", pid).Scan(&version, &status, &current)
			if e != nil {
				dbFail(w, e)
				return
			}
			if !checkVersion(in.UpdatedAt, version) || action == "assign" && (current != nil || dismissed || status != "active") || action == "unassign" && (current == nil || *current != cid) {
				fail(w, 409)
				return
			}
			if action == "assign" {
				_, e = tx.Exec(ctx, "UPDATE positions SET collaborator_id=$2,assignment_start_date=coalesce($3::date,current_date) WHERE id=$1", pid, cid, in.Date)
			} else {
				_, e = tx.Exec(ctx, "UPDATE positions SET collaborator_id=NULL,assignment_finish_date=coalesce($2::date,current_date) WHERE id=$1", pid, in.Date)
			}
			if e != nil {
				structureError(w, e)
				return
			}
			out, e := assignmentState(ctx, tx, cid)
			if e == nil {
				out["position"], e = structureRead(ctx, tx, "positions", pid)
			}
			if e == nil {
				e = tx.Commit(ctx)
			}
			if e != nil {
				dbFail(w, e)
				return
			}
			reply(w, 200, out)
		})
	}
	handle("PUT /admin/collaborators/{id}/main-position", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PositionID *string `json:"position_id"`
			UpdatedAt  string  `json:"updated_at"`
		}
		if decode(r, &in) != nil {
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
		cid := r.PathValue("id")
		var version time.Time
		var old *string
		e = tx.QueryRow(ctx, "SELECT updated_at,main_position_id::text FROM collaborators WHERE id=$1 FOR UPDATE", cid).Scan(&version, &old)
		if e != nil {
			dbFail(w, e)
			return
		}
		if !checkVersion(in.UpdatedAt, version) {
			fail(w, 409)
			return
		}
		rows, e := tx.Query(ctx, "SELECT id FROM positions WHERE id=$1 OR id=$2 ORDER BY id FOR UPDATE", old, in.PositionID)
		if e != nil {
			dbFail(w, e)
			return
		}
		rows.Close()
		if rows.Err() != nil {
			dbFail(w, rows.Err())
			return
		}
		if in.PositionID != nil {
			var owner *string
			if e = tx.QueryRow(ctx, "SELECT collaborator_id::text FROM positions WHERE id=$1", in.PositionID).Scan(&owner); e != nil {
				dbFail(w, e)
				return
			}
			if owner == nil || *owner != cid {
				fail(w, 409)
				return
			}
		}
		_, e = tx.Exec(ctx, "UPDATE collaborators SET main_position_id=$2 WHERE id=$1", cid, in.PositionID)
		if e != nil {
			structureError(w, e)
			return
		}
		out, e := assignmentState(ctx, tx, cid)
		if e == nil {
			e = tx.Commit(ctx)
		}
		if e != nil {
			dbFail(w, e)
			return
		}
		reply(w, 200, out)
	})
}

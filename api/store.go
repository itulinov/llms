package main

import (
	"context"
	"crypto/sha256"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type user struct {
	ID             string  `json:"id"`
	Login          string  `json:"login"`
	Role           string  `json:"role"`
	CollaboratorID *string `json:"collaborator_id"`
	Fullname       *string `json:"fullname"`
}
type authStore interface {
	credentials(context.Context, string) (user, string, bool, error)
	session(context.Context, []byte) (user, error)
	create(context.Context, string, []byte, time.Duration) error
	revoke(context.Context, []byte) error
}
type pgAuthStore struct{ pool *pgxpool.Pool }

const userColumns = "a.id::text,a.login,a.role,a.collaborator_id::text,c.fullname"

func (s pgAuthStore) credentials(ctx context.Context, login string) (u user, hash string, active bool, err error) {
	err = s.pool.QueryRow(ctx, "SELECT "+userColumns+",a.password_hash,a.is_active FROM accounts a LEFT JOIN v_collaborators c ON c.id=a.collaborator_id WHERE lower(a.login)=$1", login).Scan(&u.ID, &u.Login, &u.Role, &u.CollaboratorID, &u.Fullname, &hash, &active)
	return
}
func (s pgAuthStore) session(ctx context.Context, token []byte) (u user, err error) {
	h := sha256.Sum256(token)
	err = s.pool.QueryRow(ctx, "SELECT "+userColumns+" FROM auth_sessions s JOIN accounts a ON a.id=s.account_id LEFT JOIN v_collaborators c ON c.id=a.collaborator_id WHERE s.token_hash=$1 AND s.expires_at>now() AND a.is_active", h[:]).Scan(&u.ID, &u.Login, &u.Role, &u.CollaboratorID, &u.Fullname)
	return
}
func (s pgAuthStore) create(ctx context.Context, id string, token []byte, ttl time.Duration) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var active bool
	var hash string
	if err = tx.QueryRow(ctx, "SELECT is_active,password_hash FROM accounts WHERE id=$1 FOR UPDATE", id).Scan(&active, &hash); err != nil {
		return err
	}
	if !active {
		return pgx.ErrNoRows
	}
	expected, _ := ctx.Value(verifiedHashKey{}).(string)
	if expected != "" && expected != hash {
		return pgx.ErrNoRows
	}
	h := sha256.Sum256(token)
	_, err = tx.Exec(ctx, "INSERT INTO auth_sessions(account_id,token_hash,expires_at) VALUES($1,$2,now()+$3::bigint*interval '1 microsecond')", id, h[:], ttl.Microseconds())
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s pgAuthStore) revoke(ctx context.Context, token []byte) error {
	h := sha256.Sum256(token)
	_, err := s.pool.Exec(ctx, "DELETE FROM auth_sessions WHERE token_hash=$1", h[:])
	return err
}

type verifiedHashKey struct{}

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

// AUTH_TEST_DATABASE_URL должен указывать на служебную БД с правом CREATE DATABASE.
// Тест создаёт собственную БД и никогда не меняет таблицы указанной БД.
func TestPostgresSessions(t *testing.T) {
	raw := os.Getenv("AUTH_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("AUTH_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, e := pgx.Connect(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(context.Background())
	b := make([]byte, 8)
	if _, e = rand.Read(b); e != nil {
		t.Fatal(e)
	}
	name := "lms_auth_test_" + hex.EncodeToString(b)
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_, e := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		if e != nil {
			t.Error(e)
		}
	}()
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Database = name
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema, e := os.ReadFile("../postgres/schema.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(schema)); e != nil {
		t.Fatal(e)
	}
	var c, id string
	if e = pool.QueryRow(ctx, "INSERT INTO collaborators(lastname,firstname) VALUES('Иванов','Иван') RETURNING id::text").Scan(&c); e != nil {
		t.Fatal(e)
	}
	hash, _ := hashPassword("test-password-123")
	if e = pool.QueryRow(ctx, "INSERT INTO accounts(login,password_hash,role,collaborator_id) VALUES('employee',$1,'employee',$2) RETURNING id::text", hash, c).Scan(&id); e != nil {
		t.Fatal(e)
	}
	s := pgAuthStore{pool}
	u, h, active, e := s.credentials(ctx, "employee")
	if e != nil || h != hash || !active || u.Fullname == nil || *u.Fullname != "Иванов Иван" {
		t.Fatal(u, e)
	}
	token := make([]byte, 32)
	rand.Read(token)
	if e = s.create(ctx, id, token, time.Hour); e != nil {
		t.Fatal(e)
	}
	other, e := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if _, e = (pgAuthStore{other}).session(ctx, token); e != nil {
		t.Fatal("session not shared", e)
	}
	if _, e = pool.Exec(ctx, "UPDATE accounts SET role='admin' WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	u, e = s.session(ctx, token)
	if e != nil || u.Role != "admin" {
		t.Fatal(u, e)
	}
	pool.Exec(ctx, "UPDATE accounts SET is_active=false WHERE id=$1", id)
	if _, e = s.session(ctx, token); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal(e)
	}
	pool.Exec(ctx, "UPDATE accounts SET is_active=true WHERE id=$1", id)
	if e = s.revoke(ctx, token); e != nil {
		t.Fatal(e)
	}
	if _, e = s.session(ctx, token); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal(e)
	}
	if e = s.create(ctx, id, token, time.Microsecond); e != nil {
		t.Fatal(e)
	}
	// Ожидание срока не требуется: следующий запрос начинается после INSERT.
	if _, e = s.session(ctx, token); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal(e)
	}
}

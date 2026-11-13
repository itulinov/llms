package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

var errAdminExists = errors.New("администратор уже существует")

// Блокировка сериализует конкурирующие процессы создания первого администратора.
func createFirstAdmin(ctx context.Context, pool *pgxpool.Pool, login, password string) error {
	login, err := normalizeLogin(login)
	if err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(2026111301)"); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM accounts WHERE role='admin')").Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errAdminExists
	}
	if _, err = tx.Exec(ctx, "INSERT INTO accounts(login,password_hash,role) VALUES($1,$2,'admin')", login, hash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func cleanupSessions(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	result, err := pool.Exec(ctx, "DELETE FROM auth_sessions WHERE expires_at<=now()")
	return result.RowsAffected(), err
}

// CLI не требует браузерных AUTH_* настроек и не запускает HTTP-сервер.
func runCLI(args []string, input *os.File, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("ожидалась команда")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(output)
	var login string
	switch args[0] {
	case "create-admin":
		flags.StringVar(&login, "login", "", "логин первого администратора")
	case "cleanup-sessions":
	default:
		return errors.New("неизвестная команда; доступны create-admin и cleanup-sessions")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("неожиданные аргументы")
	}
	var password string
	if args[0] == "create-admin" {
		var err error
		login, err = normalizeLogin(login)
		if err != nil {
			return errors.New("укажите корректный --login")
		}
		if !term.IsTerminal(int(input.Fd())) {
			return errors.New("пароль можно ввести только в терминале")
		}
		fmt.Fprint(output, "Пароль: ")
		first, err := term.ReadPassword(int(input.Fd()))
		fmt.Fprintln(output)
		if err != nil {
			return errors.New("не удалось прочитать пароль")
		}
		defer clear(first)
		fmt.Fprint(output, "Повторите пароль: ")
		second, err := term.ReadPassword(int(input.Fd()))
		fmt.Fprintln(output)
		if err != nil {
			return errors.New("не удалось прочитать подтверждение")
		}
		defer clear(second)
		if string(first) != string(second) {
			return errors.New("пароли не совпадают")
		}
		password = string(first)
		if !validPassword(password) {
			return errors.New("пароль должен содержать 12–128 Unicode-символов")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, "")
	if err != nil {
		return errors.New("не удалось настроить PostgreSQL")
	}
	defer pool.Close()
	if args[0] == "create-admin" {
		err = createFirstAdmin(ctx, pool, login, password)
		if errors.Is(err, errAdminExists) {
			return err
		}
		if err != nil {
			return errors.New("не удалось создать администратора; проверьте PostgreSQL, миграции и уникальность логина")
		}
		fmt.Fprintln(output, "Первый администратор создан")
	} else {
		n, err := cleanupSessions(ctx, pool)
		if err != nil {
			return errors.New("не удалось очистить сессии; проверьте PostgreSQL и миграции")
		}
		fmt.Fprintf(output, "Удалено просроченных сессий: %d\n", n)
	}
	return nil
}

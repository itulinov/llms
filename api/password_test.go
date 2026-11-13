package main

import "testing"

func TestPasswords(t *testing.T) {
	p := " парольПароль12 "
	hash, err := hashPassword(p)
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(hash, p) || checkPassword(hash, "парольПароль12") {
		t.Fatal("password normalization or comparison")
	}
	other, _ := hashPassword(p)
	if hash == other {
		t.Fatal("salt reused")
	}
	if checkPassword("$argon2id$v=19$m=999999999,t=2,p=1$x$x", p) {
		t.Fatal("unsafe hash accepted")
	}
	for _, s := range []string{"short", string([]byte{255}), "12345678901"} {
		if validPassword(s) {
			t.Fatal("invalid password accepted")
		}
	}
	if !validPassword("яясимволы12345") {
		t.Fatal("unicode rejected")
	}
}
func TestLogin(t *testing.T) {
	got, err := normalizeLogin(" Admin ")
	if err != nil || got != "admin" {
		t.Fatal(got, err)
	}
	for _, s := range []string{"ab", "админ", "a b", "a/b"} {
		if _, err := normalizeLogin(s); err == nil {
			t.Fatal(s)
		}
	}
}

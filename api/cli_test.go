package main

import (
	"bytes"
	"os"
	"testing"
)

func TestCLIRejectsArguments(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"create-admin"}, {"create-admin", "--login", "bad login"}, {"create-admin", "--login", "admin", "--password", "secret"}, {"cleanup-sessions", "unexpected"}} {
		var output bytes.Buffer
		if err := runCLI(args, os.Stdin, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestCLIRejectsPipe(t *testing.T) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var output bytes.Buffer
	if err := runCLI([]string{"create-admin", "--login", "admin"}, input, &output); err == nil {
		t.Fatal("non-terminal accepted")
	}
}

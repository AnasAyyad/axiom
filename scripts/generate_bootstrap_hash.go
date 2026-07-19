//go:build ignore

// Command generate_bootstrap_hash reads one password from stdin and emits only
// its current A11 Argon2id PHC hash. It is a provisioning tool, not runtime code.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"axiom/internal/authentication"
)

func main() {
	value, err := io.ReadAll(io.LimitReader(bufio.NewReader(os.Stdin), 1025))
	password := strings.TrimRight(string(value), "\r\n")
	if err != nil || len(password) == 0 || len(password) > 1024 {
		fmt.Fprintln(os.Stderr, "bootstrap_password_input_invalid")
		os.Exit(1)
	}
	hash, err := (authentication.PasswordHasher{}).Hash(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap_password_hash_failed")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, hash)
}

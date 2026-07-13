package main

import (
	"os"

	"axiom/internal/bootstrap"
)

func main() {
	os.Exit(bootstrap.Main(os.Args[1:], os.Stdout, os.Stderr))
}

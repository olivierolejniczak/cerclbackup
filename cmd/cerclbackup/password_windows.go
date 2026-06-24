//go:build windows

package main

import (
	"golang.org/x/term"
	"os"
)

func readPassword() (string, error) {
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	return string(b), err
}

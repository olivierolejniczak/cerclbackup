//go:build !windows

package main

import (
	"bufio"
	"os"

	"golang.org/x/term"
)

func readPassword() (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
	// Not a TTY (CI, pipe): fall back to plain line read.
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	return line, err
}

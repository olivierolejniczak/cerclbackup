//go:build windows

package main

import (
	"bufio"
	"os"

	"golang.org/x/term"
)

func readPassword() (string, error) {
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err == nil {
		return string(b), nil
	}
	// stdin is a pipe or the console handle is unavailable (e.g. PowerShell
	// piping to a native exe).  Fall back to reading a plain line so that
	// automation and tests can supply the password via stdin without echo.
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if e := scanner.Err(); e != nil {
		return "", e
	}
	return "", nil
}

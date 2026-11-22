//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf: a program újraindítja önmagát ugyanazzal a folyamatazonosítóval
// (Dockerben is működik, nem kell hozzá újraindítási szabály).
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

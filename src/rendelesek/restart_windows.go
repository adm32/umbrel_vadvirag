//go:build windows

package main

import (
	"os"
	"os/exec"
)

// restartSelf: új példány indítása, majd kilépés. Az új példány pár másodpercig
// újrapróbálja a port lefoglalását, amíg ez a folyamat el nem engedi.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append(os.Args[1:], "-nobrowser")...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

//go:build windows

package main

import "syscall"

func init() {
	// UTF-8 a konzolon, hogy az ékezetek jól jelenjenek meg
	syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleOutputCP").Call(65001)
}

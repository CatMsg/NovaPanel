//go:build darwin || linux

package main

import "syscall"

func setPrivateUmask() {
	syscall.Umask(0o077)
}

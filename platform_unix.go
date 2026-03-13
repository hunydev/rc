//go:build !windows

package main

import "syscall"

const defaultShell = "bash"

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

//go:build windows

package main

import "syscall"

const defaultShell = "cmd.exe"

func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

//go:build !windows

package dolt

import "syscall"

func doltSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}

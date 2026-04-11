package main

import (
	"syscall"
)

type syscallStatfs struct {
	Bsize  int64
	Blocks uint64
	Bavail uint64
}

func statfs(path string, buf *syscallStatfs) error {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return err
	}
	buf.Bsize = s.Bsize
	buf.Blocks = s.Blocks
	buf.Bavail = s.Bavail
	return nil
}

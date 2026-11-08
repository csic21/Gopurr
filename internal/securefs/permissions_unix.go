//go:build !windows

package securefs

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"gopurr/internal/localize"
)

func restrict(path string, directory bool) error {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !directory && !info.Mode().IsRegular() {
		return fmt.Errorf(localize.Text("不是普通文件: %s"), path)
	}
	if !directory {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return err
		}
		if stat.Nlink != 1 {
			return fmt.Errorf(localize.Text("拒绝访问硬链接文件: %s"), path)
		}
	}
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return f.Chmod(mode)
}

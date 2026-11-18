package elm

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func statGroup(fi os.FileInfo) (string, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	g, err := user.LookupGroupId(strconv.Itoa(int(st.Gid)))
	if err != nil {
		return "", false
	}
	return g.Name, true
}

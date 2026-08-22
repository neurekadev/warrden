//go:build linux

package app

import (
	"os"
	"strconv"
)

func testIdentity() (string, string) {
	if os.Geteuid() == 0 {
		return "0", "0"
	}
	return strconv.Itoa(os.Geteuid()), strconv.Itoa(os.Getegid())
}

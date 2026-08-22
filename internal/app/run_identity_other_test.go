//go:build !linux

package app

func testIdentity() (string, string) { return "0", "0" }

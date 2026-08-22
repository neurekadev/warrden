//go:build !linux

package identity

func apply(_ string, _ ids) error { return nil }

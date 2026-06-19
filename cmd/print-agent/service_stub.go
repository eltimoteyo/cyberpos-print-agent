//go:build !windows

package main

import "log"

func runAsService() bool {
	return false
}

func runService(name string, isDebug bool) {
	log.Fatalf("service mode is only supported on Windows")
}

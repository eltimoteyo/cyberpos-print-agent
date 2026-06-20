//go:build !windows

package main

import "log"

func selfInstall(_ []string) {
	log.Fatal("install solo está disponible en Windows")
}

func selfUninstall() {
	log.Fatal("uninstall solo está disponible en Windows")
}

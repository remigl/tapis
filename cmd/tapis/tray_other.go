//go:build !linux

package main

import (
	"log"

	"github.com/remigl/tapis/internal/server"
)

func startTray(*server.Server, *log.Logger, string, func()) {}
func trayRefresh()                                          {}
func desktopConfirm(string, string, string) bool            { return false }

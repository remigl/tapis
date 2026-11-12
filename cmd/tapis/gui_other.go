//go:build !windows

package main

import (
	"log"

	"github.com/remigl/tapis/internal/server"
)

func guiAvailable() bool                                          { return false }
func alreadyRunning() bool                                        { return false }
func guiError(error)                                              {}
func guiConfirm(string, string, string) bool                      { return false }
func guiPairingUsed()                                             {}
func runGUI(*server.Server, *log.Logger, int, string, bool, bool) {}

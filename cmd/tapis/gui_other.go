//go:build !windows

package main

import (
	"io"
	"log"

	"github.com/remigl/tapis/internal/server"
)

func guiAvailable() bool                                          { return false }
func alreadyRunning() bool                                        { return false }
func openLog(string) io.Writer                                    { return io.Discard }
func guiError(error)                                              {}
func guiConfirm(string, string) bool                              { return false }
func guiPairingUsed()                                             {}
func runGUI(*server.Server, *log.Logger, int, string, bool, bool) {}

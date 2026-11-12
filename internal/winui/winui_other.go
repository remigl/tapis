//go:build !windows

package winui

import "errors"

func IsFrench() bool                                           { return false }
func MessageBox(title, text string, question, isErr bool) bool { return false }
func SignalRunningInstance() bool                              { return false }
func AutostartPath() string                                    { return "" }
func SetAutostart(bool) error                                  { return errors.New("not supported") }
func RefreshAutostart()                                        {}
func OpenPath(string)                                          {}
func Run(Options) error                                        { return errors.New("not supported") }
func ShowQR(Pairing)                                           {}
func CloseQR()                                                 {}
func Notify(title, text string)                                {}
func Quit()                                                    {}

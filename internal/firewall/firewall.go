// Package firewall makes sure the operating system firewall lets phones on
// the local network reach Tapis, without opening it to anything else.
package firewall

// Status describes the outcome of Ensure.
type Status int

const (
	// NotApplicable: nothing to do on this platform.
	NotApplicable Status = iota
	// AlreadyOK: a suitable rule already exists.
	AlreadyOK
	// Configured: the rule was created now.
	Configured
	// Failed: the rule could not be created (see the returned error).
	Failed
)

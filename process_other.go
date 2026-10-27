//go:build !windows

package main

import "errors"

var errUnsupported = errors.New("game detection is only supported on Windows")

func scanProcesses() ([]Process, SystemState, error) {
	return nil, SystemState{}, errUnsupported
}

//go:build !linux && !darwin && !windows

package config

import "errors"

func readMachineID() (string, error) { return "", errors.New("no machine id on this platform") }

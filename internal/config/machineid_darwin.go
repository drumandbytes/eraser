//go:build darwin

package config

import (
	"errors"
	"os/exec"
	"regexp"
)

var platformUUIDRe = regexp.MustCompile(`"IOPlatformUUID" = "([^"]+)"`)

func readMachineID() (string, error) {
	out, err := exec.Command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", err
	}
	m := platformUUIDRe.FindSubmatch(out)
	if m == nil {
		return "", errors.New("IOPlatformUUID not found")
	}
	return string(m[1]), nil
}

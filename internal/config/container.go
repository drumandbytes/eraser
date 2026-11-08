package config

import (
	"os"
	"strings"
)

// ContainerEnv is set to "1" by the Docker image (see docs/docker.md). It
// switches on what only holds inside a container: Chrome runs without its own
// sandbox (Docker's default seccomp profile denies it the user namespaces it
// needs) and with /dev/shm disabled, and there's no launchd/systemd to install
// a schedule into. A desktop install never sets it.
const ContainerEnv = "ERASER_CONTAINER"

// InContainer reports whether ContainerEnv is "1" or "true".
func InContainer() bool { return containerFlag(os.Getenv(ContainerEnv)) }

func containerFlag(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	}
	return false
}

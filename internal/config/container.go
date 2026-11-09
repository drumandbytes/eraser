package config

import "os"

// ContainerEnv is set to "1" by the Docker image (see docs/docker.md). It
// tells Eraser it has no launchd/systemd to install a schedule into (the web
// UI and `schedule install` hide that option) and no local Chrome, so a
// browser failure points at ChromeURLEnv instead of a missing executable.
const ContainerEnv = "ERASER_CONTAINER"

// ChromeURLEnv names a Chrome DevTools endpoint (http://chrome:9222 or a ws://
// URL) to attach to instead of launching a local Chrome. Works on a desktop
// too, e.g. to drive a Chrome you started with --remote-debugging-port.
const ChromeURLEnv = "ERASER_CHROME_URL"

// InContainer reports whether ContainerEnv is "1".
func InContainer() bool { return os.Getenv(ContainerEnv) == "1" }

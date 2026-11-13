package config

// ContainerEnv is set to "1" by the Docker image (see docs/docker.md). It
// tells Delistd it has no launchd/systemd to install a schedule into (the web
// UI and `schedule install` hide that option) and no local Chrome, so a
// browser failure points at ChromeURLEnv instead of a missing executable.
const ContainerEnv = "DELISTD_CONTAINER"

// ChromeURLEnv names a Chrome DevTools endpoint (http://chrome:9222 or a ws://
// URL) to attach to instead of launching a local Chrome. Works on a desktop
// too, e.g. to drive a Chrome you started with --remote-debugging-port.
const ChromeURLEnv = "DELISTD_CHROME_URL"

// AllowedHostsEnv is a comma-separated list of Host header values permitted
// by the web UI (e.g. "192.168.1.100,unraid.local" or "*" to allow any).
const AllowedHostsEnv = "DELISTD_ALLOWED_HOSTS"

// InContainer reports whether ContainerEnv is "1".
func InContainer() bool { return Getenv(ContainerEnv) == "1" }

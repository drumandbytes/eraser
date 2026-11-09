package config

import "testing"

func TestInContainer(t *testing.T) {
	t.Setenv(ContainerEnv, "1")
	if !InContainer() {
		t.Errorf("InContainer() with %s=1 = false", ContainerEnv)
	}
	t.Setenv(ContainerEnv, "")
	if InContainer() {
		t.Errorf("InContainer() with %s unset = true", ContainerEnv)
	}
}

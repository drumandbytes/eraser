package config

import "testing"

func TestContainerFlag(t *testing.T) {
	for v, want := range map[string]bool{
		"": false, "0": false, "false": false, "yes": false,
		"1": true, "true": true, "TRUE": true, " 1 ": true,
	} {
		if got := containerFlag(v); got != want {
			t.Errorf("containerFlag(%q) = %v, want %v", v, got, want)
		}
	}
}

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

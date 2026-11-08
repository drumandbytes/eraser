package browser

import "testing"

func TestContainerAllocatorOptions(t *testing.T) {
	if got := containerAllocatorOptions(false); len(got) != 0 {
		t.Errorf("desktop: %d extra Chrome options, want none", len(got))
	}
	if got := containerAllocatorOptions(true); len(got) != 3 {
		t.Errorf("container: %d extra Chrome options, want 3 (headless, no-sandbox, disable-dev-shm-usage)", len(got))
	}
}

package buildinfo

import "testing"

func TestVersionIsNotEmpty(t *testing.T) {
	if GetVersion() == "" {
		t.Fatal("version must not be empty")
	}
}

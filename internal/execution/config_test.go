package execution

import (
	"path/filepath"
	"testing"
)

func TestFrozenCostConfigMatchesDefaults(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	got, err := LoadFrozenConfig(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultConfig("test")
	if got != want {
		t.Fatalf("LoadFrozenConfig() = %+v, want %+v", got, want)
	}
}

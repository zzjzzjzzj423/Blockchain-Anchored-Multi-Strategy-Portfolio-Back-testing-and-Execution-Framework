package main

import (
	"context"
	"testing"
)

func TestRunHelpAndUnknownCommand(t *testing.T) {
	t.Parallel()

	if code := run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("help exit code = %d, want 0", code)
	}
	if code := run(context.Background(), []string{"not-a-command"}); code != 2 {
		t.Fatalf("unknown command exit code = %d, want 2", code)
	}
}

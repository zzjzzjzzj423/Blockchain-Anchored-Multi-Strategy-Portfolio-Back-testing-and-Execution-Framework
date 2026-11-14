package doctor

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReportReady(t *testing.T) {
	t.Parallel()

	required := Tool{Name: "required", Required: true}
	optional := Tool{Name: "optional", Required: false}

	if !(Report{{Tool: required}, {Tool: optional, Err: errors.New("missing")}}).Ready() {
		t.Fatal("optional missing tool must not fail the current milestone")
	}
	if (Report{{Tool: required, Err: errors.New("missing")}}).Ready() {
		t.Fatal("required missing tool must fail the current milestone")
	}
}

func TestPrintIsStableAndExplainsReadiness(t *testing.T) {
	t.Parallel()

	report := Report{
		{Tool: Tool{Name: "Git", Milestone: "M0", Required: true}, Version: "git version test"},
		{Tool: Tool{Name: "Foundry", Milestone: "M7"}, Err: errors.New("missing")},
	}
	var output bytes.Buffer
	Print(&output, report)

	for _, want := range []string{"OK", "Git", "MISSING", "Foundry", "M0 READY"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output %q does not contain %q", output.String(), want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()

	if got := firstLine("one\ntwo\n"); got != "one" {
		t.Fatalf("firstLine() = %q, want one", got)
	}
}

func TestValidateGoVersion(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"go version go1.24.0 linux/amd64",
		"go version go1.27.4 darwin/arm64",
		"go version go2.0.0 future/arch",
	} {
		if err := validateGoVersion(version); err != nil {
			t.Errorf("validateGoVersion(%q) returned %v", version, err)
		}
	}

	for _, version := range []string{
		"go version go1.23.9 linux/amd64",
		"not a Go version",
	} {
		if err := validateGoVersion(version); err == nil {
			t.Errorf("validateGoVersion(%q) returned nil", version)
		}
	}
}

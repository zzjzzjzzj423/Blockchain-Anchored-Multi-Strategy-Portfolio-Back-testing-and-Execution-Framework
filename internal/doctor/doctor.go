package doctor

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var goVersionPattern = regexp.MustCompile(`\bgo([0-9]+)\.([0-9]+)`)

// Tool describes an external executable and the first milestone that needs it.
type Tool struct {
	Name      string
	Command   string
	Args      []string
	Milestone string
	Required  bool
	Validate  func(string) error
}

// Result is one deterministic environment check result.
type Result struct {
	Tool    Tool
	Path    string
	Version string
	Err     error
}

// Report contains results in the same order as the supplied tool definitions.
type Report []Result

// Ready reports whether every tool required by the current milestone is usable.
func (r Report) Ready() bool {
	for _, result := range r {
		if result.Tool.Required && result.Err != nil {
			return false
		}
	}
	return true
}

// DefaultTools returns the M0 requirements and tools needed by later milestones.
func DefaultTools() []Tool {
	return []Tool{
		{Name: "Git", Command: "git", Args: []string{"--version"}, Milestone: "M0", Required: true},
		{Name: "Go >=1.24", Command: "go", Args: []string{"version"}, Milestone: "M0", Required: true, Validate: validateGoVersion},
		{Name: "Foundry", Command: "forge", Args: []string{"--version"}, Milestone: "M7"},
		{Name: "Anvil", Command: "anvil", Args: []string{"--version"}, Milestone: "M7"},
		{Name: "Node", Command: "node", Args: []string{"--version"}, Milestone: "M4"},
		{Name: "Codex", Command: "codex", Args: []string{"--version"}, Milestone: "M4"},
	}
}

// Check resolves each executable and reads its version without mutating the host.
func Check(ctx context.Context, tools []Tool) Report {
	report := make(Report, 0, len(tools))
	for _, tool := range tools {
		result := Result{Tool: tool}
		path, err := exec.LookPath(tool.Command)
		if err != nil {
			result.Err = err
			report = append(report, result)
			continue
		}
		result.Path = path

		output, err := exec.CommandContext(ctx, path, tool.Args...).Output()
		if err != nil {
			result.Err = err
		} else {
			result.Version = firstLine(string(output))
			if tool.Validate != nil {
				result.Err = tool.Validate(result.Version)
			}
		}
		report = append(report, result)
	}
	return report
}

// Print writes a stable, human-readable report.
func Print(w io.Writer, report Report) {
	fmt.Fprintln(w, "Toolchain doctor")
	fmt.Fprintln(w, "STATUS   TOOL           NEEDED  VERSION")
	for _, result := range report {
		status := "OK"
		version := result.Version
		if result.Err != nil {
			status = "MISSING"
			version = "-"
		}
		fmt.Fprintf(w, "%-8s %-14s %-7s %s\n", status, result.Tool.Name, result.Tool.Milestone, version)
	}
	if report.Ready() {
		fmt.Fprintln(w, "M0 READY: all current requirements are available")
	} else {
		fmt.Fprintln(w, "M0 NOT READY: install the missing M0 tools")
	}
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	return value
}

func validateGoVersion(value string) error {
	matches := goVersionPattern.FindStringSubmatch(value)
	if len(matches) != 3 {
		return fmt.Errorf("cannot parse Go version from %q", value)
	}
	major, majorErr := strconv.Atoi(matches[1])
	minor, minorErr := strconv.Atoi(matches[2])
	if majorErr != nil || minorErr != nil {
		return fmt.Errorf("cannot parse Go version from %q", value)
	}
	if major < 1 || (major == 1 && minor < 24) {
		return fmt.Errorf("Go 1.24 or newer is required; found %s", matches[0])
	}
	return nil
}

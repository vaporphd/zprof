package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewShakedownCmd() *cobra.Command {
	var (
		model   string
		verbose bool
	)
	c := &cobra.Command{
		Use:   "shakedown --general",
		Short: "Smoke-test agent contracts on a fixture project",
		Long: `Runs the full agent chain (task-runner → planner → implementer →
tester → reviewer) on a minimal fixture project without any external
toolchain. Checks contract compliance: verdict format, no preamble,
artifact exists, run log created, no recursive runner.

Uses haiku by default for cost (~10K tokens).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGeneralShakedown(cmd, model, verbose)
		},
	}
	c.Flags().StringVar(&model, "model", "haiku", "Model for the shakedown run")
	c.Flags().BoolVar(&verbose, "verbose", false, "Show claude output")
	return c
}

type shakedownCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func runGeneralShakedown(cmd *cobra.Command, model string, verbose bool) error {
	// Find fixture
	repoDir := repoDir()
	fixtureSrc := filepath.Join(repoDir, "base", "shakedown", "project")
	taskFile := filepath.Join(repoDir, "base", "shakedown", "task.md")

	if _, err := os.Stat(fixtureSrc); err != nil {
		return fmt.Errorf("fixture not found at %s — is ZPROF_REPO set?", fixtureSrc)
	}

	taskBytes, err := os.ReadFile(taskFile)
	if err != nil {
		return fmt.Errorf("read task: %w", err)
	}

	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "zprof-shakedown-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Copy fixture
	if err := copyDir(fixtureSrc, tmpDir); err != nil {
		return fmt.Errorf("copy fixture: %w", err)
	}

	// Init git (task-runner needs it for run logs)
	runCmd(tmpDir, "git", "init", "-q")
	runCmd(tmpDir, "git", "add", ".")
	runCmd(tmpDir, "git", "commit", "-q", "-m", "initial")

	// Apply zprof with a minimal overlay
	zprofBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find zprof binary: %w", err)
	}
	applyCmd := exec.Command(zprofBin, "apply", "backend-python")
	applyCmd.Dir = tmpDir
	applyCmd.Env = append(os.Environ(), "ZPROF_REPO="+repoDir)
	applyOut, err := applyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zprof apply: %s\n%s", err, applyOut)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "fixture prepared at %s\n", tmpDir)
	fmt.Fprintf(cmd.ErrOrStderr(), "running shakedown with model=%s...\n", model)

	// Run claude on the task
	start := time.Now()
	claudeArgs := []string{"-p", "--dangerously-skip-permissions", "--model", model}
	claudeCmd := exec.Command("claude", claudeArgs...)
	claudeCmd.Dir = tmpDir
	claudeCmd.Stdin = strings.NewReader(string(taskBytes))

	output, err := claudeCmd.CombinedOutput()
	elapsed := time.Since(start)

	if verbose {
		fmt.Fprintln(cmd.OutOrStdout(), string(output))
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "completed in %s (exit=%v)\n", elapsed.Round(time.Second), err)

	// Run checks
	checks := runShakedownChecks(tmpDir, string(output))

	// Report
	passed := 0
	for _, ch := range checks {
		status := "✓"
		if !ch.Passed {
			status = "✗"
		} else {
			passed++
		}
		detail := ""
		if ch.Detail != "" {
			detail = " — " + ch.Detail
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s %s%s\n", status, ch.Name, detail)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n%d/%d checks passed\n", passed, len(checks))

	if passed < len(checks) {
		return fmt.Errorf("shakedown failed: %d/%d", passed, len(checks))
	}
	return nil
}

func runShakedownChecks(projectDir, output string) []shakedownCheck {
	var checks []shakedownCheck

	// Check: run log exists
	runLogs, _ := filepath.Glob(filepath.Join(projectDir, ".zprof", "runs", "*.md"))
	checks = append(checks, shakedownCheck{
		Name:   "run log created",
		Passed: len(runLogs) > 0,
		Detail: fmt.Sprintf("%d run log(s)", len(runLogs)),
	})

	// Check: output starts with verdict
	lines := strings.Split(strings.TrimSpace(output), "\n")
	startsWithVerdict := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		startsWithVerdict = strings.HasPrefix(strings.ToLower(trimmed), "verdict:")
		break
	}
	checks = append(checks, shakedownCheck{
		Name:   "verdict in first non-empty line",
		Passed: startsWithVerdict,
		Detail: firstN(lines, 1),
	})

	// Check: artifact referenced
	hasArtifact := strings.Contains(strings.ToLower(output), "artifact:")
	checks = append(checks, shakedownCheck{
		Name:   "artifact referenced",
		Passed: hasArtifact,
	})

	// Check: main.py was modified (divide validation added)
	mainPy, _ := os.ReadFile(filepath.Join(projectDir, "main.py"))
	hasValidation := strings.Contains(string(mainPy), "ValueError") ||
		strings.Contains(string(mainPy), "ZeroDivision") ||
		strings.Contains(string(mainPy), "raise") ||
		strings.Contains(string(mainPy), "if b == 0") ||
		strings.Contains(string(mainPy), "if b == 0.0")
	checks = append(checks, shakedownCheck{
		Name:   "divide validation added to main.py",
		Passed: hasValidation,
	})

	// Check: test added
	testPy, _ := os.ReadFile(filepath.Join(projectDir, "test_main.py"))
	hasNewTest := strings.Contains(string(testPy), "zero") ||
		strings.Contains(string(testPy), "Zero") ||
		strings.Contains(string(testPy), "ValueError")
	checks = append(checks, shakedownCheck{
		Name:   "divide-by-zero test added",
		Passed: hasNewTest,
	})

	// Check: tests pass
	testCmd := exec.Command("python3", "-m", "pytest", "test_main.py", "-v")
	testCmd.Dir = projectDir
	testOut, testErr := testCmd.CombinedOutput()
	checks = append(checks, shakedownCheck{
		Name:   "tests pass after changes",
		Passed: testErr == nil,
		Detail: lastLine(string(testOut)),
	})

	// Check: subagent dispatches in session logs
	claudeDir := filepath.Join(projectDir, ".claude")
	if _, err := os.Stat(claudeDir); err == nil {
		// Check for agent dispatch evidence
		checks = append(checks, shakedownCheck{
			Name:   ".claude/ directory exists",
			Passed: true,
		})
	}

	// Check: no recursive runner (task-runner spawning task-runner)
	checks = append(checks, shakedownCheck{
		Name:   "no recursive runner",
		Passed: !strings.Contains(output, "task-runner → task-runner"),
		Detail: "checked output for recursive dispatch",
	})

	return checks
}

func runCmd(dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Run()
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dp, 0o755); err != nil {
				return err
			}
			if err := copyDir(sp, dp); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(sp)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dp, data, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstN(lines []string, n int) string {
	if len(lines) == 0 {
		return ""
	}
	if n > len(lines) {
		n = len(lines)
	}
	return strings.Join(lines[:n], " | ")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}


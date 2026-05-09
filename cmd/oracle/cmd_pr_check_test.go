package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"CloudOracle/internal/config"
	"CloudOracle/internal/diff"
)

// erroringSource satisfies diff.Source by always returning an error.
// Pricing engine reacts by marking every resource as Skipped: estimation
// failed, which is enough to exercise the orchestration paths in
// runPRCheck without standing up real pricing fixtures or an AWS SDK.
// We test the rest of the CostDiff machinery in internal/diff and
// internal/pricing with their own fixtures — repeating those tests at
// the cmd layer would only retest mocks.
type erroringSource struct{}

func (erroringSource) GetProducts(_ context.Context, _ string, _ map[string]string) ([]string, error) {
	return nil, errors.New("erroringSource: pricing not wired in tests")
}

// withFakeSource swaps the package-level newPRCheckSource for one that
// returns the given diff.Source. The original factory is restored on
// test cleanup so tests stay independent.
func withFakeSource(t *testing.T, src diff.Source) {
	t.Helper()
	prev := newPRCheckSource
	newPRCheckSource = func(_ context.Context) (diff.Source, error) {
		return src, nil
	}
	t.Cleanup(func() { newPRCheckSource = prev })
}

// captureLogs replaces slog's default logger with one that writes to a
// buffer. The buffer is returned so individual tests can assert on log
// content (e.g. that the "no LLM provider configured" line appears or
// is suppressed). Logger is restored on test cleanup.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// emptyConfig returns a config with no LLM keys set, matching how the
// CI environment will look until 16.2 wires real secrets through.
func emptyConfig() config.Config {
	return config.Config{}
}

const (
	headerMarker = "## 💰 Cloud Cost Impact"
	footerMarker = "<!-- cloudoracle-pr-v1 -->"
)

func TestPRCheck_HappyPath_Stdout(t *testing.T) {
	withFakeSource(t, erroringSource{})

	var stdout, stderr bytes.Buffer
	args := []string{
		"-plan-file=" + filepath.Join("..", "..", "internal", "iac", "testdata", "plan_simple_create.json"),
		"-no-llm",
	}
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, headerMarker) {
		t.Errorf("output missing header %q", headerMarker)
	}
	if !strings.Contains(out, footerMarker) {
		t.Errorf("output missing footer %q", footerMarker)
	}
}

func TestPRCheck_HappyPath_OutputFile(t *testing.T) {
	withFakeSource(t, erroringSource{})

	tmp := filepath.Join(t.TempDir(), "comment.md")
	args := []string{
		"-plan-file=" + filepath.Join("..", "..", "internal", "iac", "testdata", "plan_simple_create.json"),
		"-output=" + tmp,
		"-no-llm",
	}

	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("--output set but stdout still received %d bytes", stdout.Len())
	}
	body, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, headerMarker) {
		t.Errorf("file missing header marker")
	}
	if !strings.Contains(bodyStr, footerMarker) {
		t.Errorf("file missing footer marker")
	}
}

// TestPRCheck_NoLLMFlag exercises the --no-llm short-circuit. With the
// flag set, runPRCheck must not even attempt to construct an LLM
// provider — so the "no LLM provider configured" info-log produced by
// the auto-fallback path must NOT appear. Without the flag (and no
// keys configured) the same log line MUST appear, confirming we ran
// the LLM construction attempt.
func TestPRCheck_NoLLMFlag(t *testing.T) {
	withFakeSource(t, erroringSource{})
	planArg := "-plan-file=" + filepath.Join("..", "..", "internal", "iac", "testdata", "plan_simple_create.json")

	t.Run("with -no-llm: skips LLM provider construction", func(t *testing.T) {
		logs := captureLogs(t)
		var stdout, stderr bytes.Buffer
		code := runPRCheck(context.Background(), emptyConfig(),
			[]string{planArg, "-no-llm"}, &stdout, &stderr)
		if code != exitPRCheckOK {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if strings.Contains(logs.String(), "no LLM provider configured") {
			t.Errorf("--no-llm should short-circuit before LLM construction; log:\n%s", logs.String())
		}
		if strings.Contains(logs.String(), "rendering with LLM narrative") {
			t.Errorf("--no-llm should not produce LLM-rendering log; log:\n%s", logs.String())
		}
	})

	t.Run("without -no-llm: attempts LLM and falls back", func(t *testing.T) {
		logs := captureLogs(t)
		var stdout, stderr bytes.Buffer
		code := runPRCheck(context.Background(), emptyConfig(),
			[]string{planArg}, &stdout, &stderr)
		if code != exitPRCheckOK {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if !strings.Contains(logs.String(), "no LLM provider configured") {
			t.Errorf("expected slog.Info 'no LLM provider configured' when no keys + no --no-llm; log:\n%s", logs.String())
		}
	})
}

func TestPRCheck_PlanFileMissing(t *testing.T) {
	withFakeSource(t, erroringSource{})

	missing := filepath.Join(t.TempDir(), "does_not_exist.json")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(),
		[]string{"-plan-file=" + missing, "-no-llm"}, &stdout, &stderr)

	if code != exitPRCheckInputErr {
		t.Errorf("expected exit %d for missing plan file, got %d", exitPRCheckInputErr, code)
	}
	if !strings.Contains(stderr.String(), "--plan-file") {
		t.Errorf("stderr should mention the failing flag; got: %s", stderr.String())
	}
}

func TestPRCheck_PlanFileMissingFlag(t *testing.T) {
	withFakeSource(t, erroringSource{})

	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(),
		[]string{"-no-llm"}, &stdout, &stderr)

	if code != exitPRCheckInputErr {
		t.Errorf("expected exit %d when --plan-file is omitted, got %d", exitPRCheckInputErr, code)
	}
	if !strings.Contains(stderr.String(), "--plan-file is required") {
		t.Errorf("expected 'is required' message; got: %s", stderr.String())
	}
}

func TestPRCheck_PlanFileEmpty(t *testing.T) {
	withFakeSource(t, erroringSource{})

	args := []string{
		"-plan-file=" + filepath.Join("..", "..", "internal", "iac", "testdata", "plan_empty.json"),
		"-no-llm",
	}
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("empty plan should still produce a valid comment; got exit %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, headerMarker) {
		t.Errorf("empty-plan output missing header marker")
	}
	if !strings.Contains(out, "No priceable resources") {
		t.Errorf("empty-plan output should say 'No priceable resources'; got:\n%s", out)
	}
}

func TestPRCheck_OutputDirNotWritable(t *testing.T) {
	withFakeSource(t, erroringSource{})

	// A path under a non-existent parent directory: os.WriteFile will
	// reject it with a "no such file or directory"-style error. We use
	// t.TempDir() then join a nonexistent subdir — robust on Windows
	// and POSIX without depending on hardcoded /nonexistent paths.
	bad := filepath.Join(t.TempDir(), "does_not_exist_dir", "comment.md")
	args := []string{
		"-plan-file=" + filepath.Join("..", "..", "internal", "iac", "testdata", "plan_simple_create.json"),
		"-output=" + bad,
		"-no-llm",
	}
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOutputErr {
		t.Errorf("expected exit %d for unwritable output path, got %d", exitPRCheckOutputErr, code)
	}
	if !strings.Contains(stderr.String(), "--output") {
		t.Errorf("stderr should mention the --output flag on write failure; got: %s", stderr.String())
	}
}

func TestPRCheck_HelpExitsZero(t *testing.T) {
	// flag.ErrHelp is not a malformed invocation; --help is a deliberate
	// user action and should exit 0 so CI-style scripts that do
	// `oracle pr-check --help` to verify a binary works don't fail.
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(),
		[]string{"-h"}, &stdout, &stderr)
	if code != exitPRCheckOK {
		t.Errorf("--help / -h should exit 0, got %d", code)
	}
}

func TestPRCheck_FlagParseFailureExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(),
		[]string{"-not-a-real-flag=true"}, &stdout, &stderr)
	if code != exitPRCheckInputErr {
		t.Errorf("unknown flag should exit %d, got %d", exitPRCheckInputErr, code)
	}
}

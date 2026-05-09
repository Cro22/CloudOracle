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
	"CloudOracle/internal/github"
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

// --- --post flag tests ---

// postCall captures one PostOrUpdateComment invocation so a test can
// assert on what runPRCheck handed to the github layer.
type postCall struct {
	repo   github.Repo
	pr     int
	body   string
	marker string
}

// recordingGithubPoster is the test double for githubPoster: it logs
// every call and lets each test program a return value or error.
type recordingGithubPoster struct {
	calls   []postCall
	id      int64
	created bool
	err     error
}

func (r *recordingGithubPoster) PostOrUpdateComment(_ context.Context, repo github.Repo, prNumber int, body, marker string) (int64, bool, error) {
	r.calls = append(r.calls, postCall{repo: repo, pr: prNumber, body: body, marker: marker})
	if r.err != nil {
		return 0, false, r.err
	}
	id := r.id
	if id == 0 {
		id = 12345
	}
	return id, r.created, nil
}

// withFakeGithubClient swaps the package-level newPRCheckGithubClient
// for one that returns the supplied poster, capturing the token the
// factory was called with so tests can assert on env-derived values.
// The original factory is restored on test cleanup.
func withFakeGithubClient(t *testing.T, poster githubPoster) *string {
	t.Helper()
	var capturedToken string
	prev := newPRCheckGithubClient
	newPRCheckGithubClient = func(token string) githubPoster {
		capturedToken = token
		return poster
	}
	t.Cleanup(func() { newPRCheckGithubClient = prev })
	return &capturedToken
}

// commonArgs returns the boilerplate flags shared by post-flag tests.
func commonArgs(extra ...string) []string {
	base := []string{
		"-plan-file=" + filepath.Join("..", "..", "internal", "iac", "testdata", "plan_simple_create.json"),
		"-no-llm",
	}
	return append(base, extra...)
}

func TestPRCheck_PostFlag_HappyPath(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{id: 555, created: true}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=Cro22/CloudOracle", "-pr=11", "-token=test-token")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(rec.calls))
	}
	c := rec.calls[0]
	if c.repo != (github.Repo{Owner: "Cro22", Name: "CloudOracle"}) {
		t.Errorf("repo = %+v, want Cro22/CloudOracle", c.repo)
	}
	if c.pr != 11 {
		t.Errorf("pr = %d, want 11", c.pr)
	}
	if !strings.Contains(c.body, footerMarker) {
		t.Errorf("post body missing footer marker")
	}
	if c.marker != defaultMarker {
		t.Errorf("marker = %q, want default %q", c.marker, defaultMarker)
	}
	// stdout still received the markdown — --post is additive, not exclusive.
	if !strings.Contains(stdout.String(), headerMarker) {
		t.Errorf("stdout should still contain markdown when --post is set")
	}
}

func TestPRCheck_PostFlag_TokenFromEnv(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{}
	captured := withFakeGithubClient(t, rec)

	t.Setenv("GITHUB_TOKEN", "env-token")
	args := commonArgs("-post", "-repo=Cro22/CloudOracle", "-pr=11") // no -token

	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if *captured != "env-token" {
		t.Errorf("token captured = %q, want %q (from env)", *captured, "env-token")
	}
}

func TestPRCheck_PostFlag_NoToken(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{}
	withFakeGithubClient(t, rec)

	t.Setenv("GITHUB_TOKEN", "") // make sure the env doesn't satisfy the requirement
	args := commonArgs("-post", "-repo=Cro22/CloudOracle", "-pr=11")

	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckInputErr {
		t.Errorf("expected exit %d (input error), got %d", exitPRCheckInputErr, code)
	}
	if !strings.Contains(stderr.String(), "GITHUB_TOKEN") {
		t.Errorf("stderr should mention GITHUB_TOKEN; got: %s", stderr.String())
	}
	if len(rec.calls) != 0 {
		t.Errorf("github should not be called when token is missing")
	}
}

func TestPRCheck_PostFlag_BadRepoFormat(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=invalid-no-slash", "-pr=11", "-token=t")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckInputErr {
		t.Errorf("bad repo format should exit %d, got %d", exitPRCheckInputErr, code)
	}
	if !strings.Contains(stderr.String(), "owner/name") {
		t.Errorf("stderr should mention 'owner/name'; got: %s", stderr.String())
	}
	if len(rec.calls) != 0 {
		t.Errorf("github should not be called on bad repo format")
	}
}

func TestPRCheck_PostFlag_NegativePR(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=o/n", "-pr=-1", "-token=t")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckInputErr {
		t.Errorf("negative PR should exit %d, got %d", exitPRCheckInputErr, code)
	}
	if !strings.Contains(stderr.String(), "--pr") {
		t.Errorf("stderr should mention --pr; got: %s", stderr.String())
	}
}

func TestPRCheck_PostFlag_AuthError(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{
		err: errors.New("github: authentication failed (check GITHUB_TOKEN): 401 Bad credentials"),
	}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckGitHubErr {
		t.Errorf("auth error should exit %d, got %d", exitPRCheckGitHubErr, code)
	}
	if !strings.Contains(stderr.String(), "authentication") {
		t.Errorf("stderr should mention 'authentication'; got: %s", stderr.String())
	}
}

func TestPRCheck_PostFlag_NotFoundError(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{
		err: errors.New("github: repo o/n or PR #1 not found"),
	}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckGitHubErr {
		t.Errorf("not-found error should exit %d, got %d", exitPRCheckGitHubErr, code)
	}
	if !strings.Contains(stderr.String(), "not found") {
		t.Errorf("stderr should mention 'not found'; got: %s", stderr.String())
	}
}

func TestPRCheck_PostFlag_ValidationError(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{
		err: errors.New("github: validation failed: body too long"),
	}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckGitHubErr {
		t.Errorf("validation error should exit %d, got %d", exitPRCheckGitHubErr, code)
	}
	if !strings.Contains(stderr.String(), "validation") {
		t.Errorf("stderr should mention 'validation'; got: %s", stderr.String())
	}
}

func TestPRCheck_PostFlag_GenericError(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{
		err: errors.New("github: something unexpected happened"),
	}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckGitHubErr {
		t.Errorf("generic github error should exit %d, got %d", exitPRCheckGitHubErr, code)
	}
	if !strings.Contains(stderr.String(), "something unexpected happened") {
		t.Errorf("stderr should contain the underlying error message; got: %s", stderr.String())
	}
}

func TestPRCheck_PostFlag_LogsCreateVsUpdate(t *testing.T) {
	t.Run("created=true logs 'created'", func(t *testing.T) {
		withFakeSource(t, erroringSource{})
		rec := &recordingGithubPoster{id: 100, created: true}
		withFakeGithubClient(t, rec)
		logs := captureLogs(t)

		args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t")
		var stdout, stderr bytes.Buffer
		code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)
		if code != exitPRCheckOK {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if !strings.Contains(logs.String(), "created") {
			t.Errorf("logs should contain 'created' for created=true; got:\n%s", logs.String())
		}
		if strings.Contains(logs.String(), "comment updated") {
			t.Errorf("logs should NOT say 'updated' when created=true; got:\n%s", logs.String())
		}
	})

	t.Run("created=false logs 'updated'", func(t *testing.T) {
		withFakeSource(t, erroringSource{})
		rec := &recordingGithubPoster{id: 100, created: false}
		withFakeGithubClient(t, rec)
		logs := captureLogs(t)

		args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t")
		var stdout, stderr bytes.Buffer
		code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)
		if code != exitPRCheckOK {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if !strings.Contains(logs.String(), "updated") {
			t.Errorf("logs should contain 'updated' for created=false; got:\n%s", logs.String())
		}
	})
}

func TestPRCheck_NoPostFlag_DoesNotCallGithub(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{}
	withFakeGithubClient(t, rec)

	args := commonArgs() // no --post
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if len(rec.calls) != 0 {
		t.Errorf("github must not be called without --post; saw %d calls", len(rec.calls))
	}
}

func TestPRCheck_PostFlag_WritesOutputFile(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{id: 7}
	withFakeGithubClient(t, rec)

	tmp := filepath.Join(t.TempDir(), "comment.md")
	args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t", "-output="+tmp)
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	body, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if !strings.Contains(string(body), headerMarker) {
		t.Errorf("output file does not contain markdown header")
	}
	if len(rec.calls) != 1 {
		t.Errorf("expected 1 post call alongside the file write, got %d", len(rec.calls))
	}
}

func TestPRCheck_PostFlag_CustomMarker(t *testing.T) {
	withFakeSource(t, erroringSource{})
	rec := &recordingGithubPoster{}
	withFakeGithubClient(t, rec)

	args := commonArgs("-post", "-repo=o/n", "-pr=1", "-token=t", "-marker=v2-prefix")
	var stdout, stderr bytes.Buffer
	code := runPRCheck(context.Background(), emptyConfig(), args, &stdout, &stderr)

	if code != exitPRCheckOK {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 post call, got %d", len(rec.calls))
	}
	if rec.calls[0].marker != "v2-prefix" {
		t.Errorf("marker = %q, want %q", rec.calls[0].marker, "v2-prefix")
	}
}

// TestParseRepo covers the small helper directly. All paths through
// parseRepo are exercised indirectly by the post-flag tests above; the
// dedicated table here makes regressions in the validation rule
// (e.g. accepting "owner/" or "/name") surface with a sharper failure.
func TestParseRepo(t *testing.T) {
	cases := []struct {
		in       string
		wantErr  bool
		wantRepo github.Repo
	}{
		{"Cro22/CloudOracle", false, github.Repo{Owner: "Cro22", Name: "CloudOracle"}},
		{"a/b", false, github.Repo{Owner: "a", Name: "b"}},
		{"", true, github.Repo{}},
		{"no-slash", true, github.Repo{}},
		{"/missing-owner", true, github.Repo{}},
		{"missing-name/", true, github.Repo{}},
		{"a/b/c", false, github.Repo{Owner: "a", Name: "b/c"}}, // SplitN keeps trailing '/c' as Name; documented behaviour
	}
	for _, c := range cases {
		got, err := parseRepo(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseRepo(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.wantRepo {
			t.Errorf("parseRepo(%q) = %+v, want %+v", c.in, got, c.wantRepo)
		}
	}
}

package diff

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"CloudOracle/internal/llm"
)

// maxNarrativeChars caps the LLM response length at roughly three long
// sentences. The spec asks for 1-3 inline sentences; anything substantially
// longer is the LLM ignoring the instruction (or hallucinating bullets) and
// is rejected in favor of the templated fallback.
const maxNarrativeChars = 500

// promptTopMoversN is the number of top movers included in the prompt.
// More than three crowds the model with low-signal items; fewer hides
// context the model needs to identify the primary driver.
const promptTopMoversN = 3

// preamblePrefixes are common conversational openers some models tack on
// despite "no preamble" instructions. Stripped case-insensitively. Order
// matters: longer, more specific prefixes are listed first so they match
// before their shorter substrings.
var preamblePrefixes = []string{
	"Here is the narrative:",
	"Here's the narrative:",
	"Here is the narrative",
	"Here's the narrative",
	"Here is the summary:",
	"Here's the summary:",
	"Here is:",
	"Here's:",
	"Here is",
	"Here's",
	"Certainly,",
	"Certainly.",
	"Certainly!",
	"Of course,",
	"Of course.",
	"Of course!",
	"Sure,",
	"Sure.",
	"Sure!",
}

// RenderMarkdownWithLLM is RenderMarkdown but uses an LLM provider to
// generate the narrative paragraph instead of the templated default.
//
// On any LLM error (network, rate limit, parse error, empty/whitespace
// response, oversized response, context cancellation), it falls back
// silently to the templated narrative produced by RenderMarkdown — the
// fallback ensures CloudOracle always emits a valid PR comment, even
// when the LLM is unavailable. Failures are logged at slog.Warn for
// debugging; the comment never carries an "LLM failed" notice.
//
// If provider is nil, behavior is identical to RenderMarkdown — no LLM
// call is attempted and no warning is logged.
//
// Sanity checks applied to the LLM output before it replaces the
// template:
//
//   - empty / whitespace-only responses are treated as failure
//   - responses longer than 500 characters are treated as failure
//     (the LLM ignored the "1-3 sentences" instruction)
//   - common preambles ("Here is the narrative:", "Sure,", ...) are
//     stripped from the front
//   - leading/trailing whitespace is trimmed
//   - paragraph breaks pass but emit a warn (we expect inline prose)
func RenderMarkdownWithLLM(ctx context.Context, d CostDiff, provider llm.Provider) string {
	return RenderMarkdownWithLLMConfig(ctx, d, provider, MarkdownConfig{})
}

// RenderMarkdownWithLLMConfig is RenderMarkdownWithLLM with explicit
// configuration. See MarkdownConfig and RenderMarkdownWithLLM.
func RenderMarkdownWithLLMConfig(ctx context.Context, d CostDiff, provider llm.Provider, cfg MarkdownConfig) string {
	cfg = applyDefaults(cfg)
	data := buildTemplateData(d, cfg)
	if narrative, ok := generateLLMNarrative(ctx, d, provider); ok {
		data.Narrative = narrative
	}
	var buf bytes.Buffer
	if err := mdTemplate.Execute(&buf, data); err != nil {
		return fmt.Sprintf("CloudOracle render error: %v", err)
	}
	return buf.String()
}

// BuildPRNarrativePrompt constructs the prompt sent to the LLM provider
// for the PR narrative. Exposed for testability (so unit tests can
// verify prompt shape without making real API calls) and for callers
// that wire CloudOracle to an LLM not covered by internal/llm.
//
// The output is plain text — not Markdown — so it renders sensibly in
// any chat-completion or text-completion API.
func BuildPRNarrativePrompt(d CostDiff) string {
	var sb strings.Builder
	sb.WriteString("You are reviewing a Terraform pull request as a senior cloud engineer. ")
	sb.WriteString("Your output is a 1-3 sentence narrative that will appear at the top of a PR comment, ")
	sb.WriteString("above a table that already shows the per-resource cost breakdown.\n\n")

	sb.WriteString("# Cost change summary\n")
	switch {
	case d.Stats.Total == 0:
		sb.WriteString("No priceable changes in this plan (empty plan).\n\n")
	case d.Stats.Priced == 0:
		sb.WriteString(fmt.Sprintf("No priced resources in this plan (%d skipped).\n\n", d.Stats.Skipped))
	default:
		sb.WriteString("Total monthly delta: ")
		sb.WriteString(formatDelta(d.TotalMonthlyDelta))
		sb.WriteString("\nDirection: ")
		sb.WriteString(directionWord(d.TotalMonthlyDelta))
		sb.WriteString("\nStats: ")
		sb.WriteString(statsSummary(d.Stats))
		sb.WriteString("\n\n")
	}

	sb.WriteString("# Top resources by impact\n")
	if len(d.TopMovers) == 0 {
		sb.WriteString("(none)\n\n")
	} else {
		n := min(promptTopMoversN, len(d.TopMovers))
		for i := range n {
			m := d.TopMovers[i]
			sb.WriteString(fmt.Sprintf("- %s (%s, action=%s): %s per month",
				m.ResourceAddress, m.ResourceType, m.Action, formatDelta(m.MonthlyDelta)))
			if len(m.Breakdown) > 0 {
				sb.WriteString(" — components: ")
				for j, li := range m.Breakdown {
					if j > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(li.Component)
					sb.WriteByte(' ')
					sb.WriteString(formatDelta(li.MonthlyUSD))
				}
			}
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	writeCaveatsByResource(&sb, d)

	sb.WriteString("# Your task\n")
	sb.WriteString("Write 1-3 sentences that:\n")
	sb.WriteString("1. Identify the PRIMARY DRIVER of cost change (do not summarize the table; pick the dominant resource and explain its weight).\n")
	sb.WriteString("2. If a clear lower-cost alternative exists for the primary driver (smaller instance class, different storage type, etc.), mention it as an \"if X, consider Y\" — never as a prescription.\n")
	sb.WriteString("3. Optionally note one risk if applicable (e.g., uncovered cost like data processing, license assumption that may not hold).\n\n")
	sb.WriteString("DO NOT:\n")
	sb.WriteString("- Repeat the total monthly delta (it's already in the bold above your output).\n")
	sb.WriteString("- List resources by name unless they are the primary driver.\n")
	sb.WriteString("- Use cheerleading language (\"great\", \"looks good\", \"concerning\").\n")
	sb.WriteString("- Use markdown headings or lists (your output is inline prose only).\n")
	sb.WriteString("- Suggest IaC changes (\"you should add...\"); only point out cost properties.\n")
	sb.WriteString("- Suggest billing-model alternatives (Reserved Instances, Savings Plans, Spot) — those are pricing levers, not cost-shape alternatives. Limit suggestions to architectural/sizing changes (different instance class, storage type, deployment shape).\n\n")
	sb.WriteString("Output only the prose. No preamble. No \"Here is the narrative:\". Just the 1-3 sentences.")
	return sb.String()
}

// generateLLMNarrative attempts to produce a narrative via the LLM
// provider. On any failure it returns ok=false so the caller falls back
// to the templated narrative. Failures are logged at slog.Warn.
func generateLLMNarrative(ctx context.Context, d CostDiff, provider llm.Provider) (string, bool) {
	if provider == nil {
		return "", false
	}
	if err := ctx.Err(); err != nil {
		slog.Warn("PR narrative: context already cancelled before LLM call", "err", err)
		return "", false
	}
	prompt := BuildPRNarrativePrompt(d)
	raw, err := provider.GenerateText(ctx, prompt)
	if err != nil {
		slog.Warn("PR narrative: LLM provider returned error",
			"provider", provider.Name(), "err", err)
		return "", false
	}
	if err := ctx.Err(); err != nil {
		slog.Warn("PR narrative: context cancelled during LLM call",
			"provider", provider.Name(), "err", err)
		return "", false
	}
	cleaned := cleanNarrative(raw)
	if cleaned == "" {
		slog.Warn("PR narrative: LLM returned empty/whitespace response",
			"provider", provider.Name(), "raw_len", len(raw))
		return "", false
	}
	if len(cleaned) > maxNarrativeChars {
		slog.Warn("PR narrative: LLM response exceeded max length, falling back",
			"provider", provider.Name(), "len", len(cleaned), "max", maxNarrativeChars)
		return "", false
	}
	if strings.Contains(cleaned, "\n\n") {
		slog.Warn("PR narrative: LLM response contains paragraph breaks; expected inline prose",
			"provider", provider.Name())
	}
	return cleaned, true
}

// cleanNarrative trims surrounding whitespace and strips common
// conversational preambles such as "Here is the narrative:" that some
// models emit despite explicit "no preamble" instructions.
func cleanNarrative(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, p := range preamblePrefixes {
		if hasPrefixFold(s, p) {
			s = s[len(p):]
			// A stripped preamble is usually followed by stray punctuation
			// ("Here is —", "Sure - ..."). Eat one round of leading
			// punctuation/whitespace before returning.
			s = strings.TrimLeft(s, " \t:-—–.,")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

// directionWord describes the sign of the net delta for the prompt's
// "Direction:" line. Centavo-tolerance treats sub-cent fluctuations as
// neutral so the LLM does not make a fuss about floating-point noise.
func directionWord(delta float64) string {
	if math.Abs(delta) < centavoTolerance {
		return "neutral"
	}
	if delta > 0 {
		return "increase"
	}
	return "decrease (savings)"
}

// statsSummary collapses the action counters into a single human line for
// the prompt. Zero-count categories are skipped so the output is tight.
func statsSummary(s Stats) string {
	var parts []string
	if s.Created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", s.Created))
	}
	if s.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", s.Deleted))
	}
	if s.Updated > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", s.Updated))
	}
	if s.Replaced > 0 {
		parts = append(parts, fmt.Sprintf("%d replaced", s.Replaced))
	}
	if s.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", s.Skipped))
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

// caveatGroup is the prompt-shaped view of a CostDiff's notes,
// partitioned so the LLM cannot accidentally attribute one resource's
// caveat to another.
//
// The flat-list approach we used originally caused a real bug: a NAT
// Gateway caveat ("data processing charges (~$0.045/GB) not modeled")
// was attributed by the model to the RDS primary driver because the
// prompt offered no resource-level binding. Grouping by resource and
// labelling sub-blocks explicitly removes that ambiguity.
type caveatGroup struct {
	primaryAddress string
	primaryNotes   []string
	otherNotes     []resourceNote
	planWideNotes  []string
}

// resourceNote pairs a per-resource caveat with the address it belongs
// to so the prompt can render it as "{address}: {note}". The address
// prefix is what tells the LLM "this caveat is for resource X, not the
// primary driver".
type resourceNote struct {
	address string
	note    string
}

// buildCaveatGroup partitions a CostDiff's notes into the three
// sub-blocks the prompt expects. The primary driver is TopMovers[0]
// when present (TopMovers is already sorted by absolute delta and
// excludes Skipped); when TopMovers is empty there is no primary, and
// every per-resource note ends up in "other".
func buildCaveatGroup(d CostDiff) caveatGroup {
	var g caveatGroup
	g.planWideNotes = d.Notes

	var primaryAddr string
	if len(d.TopMovers) > 0 {
		primaryAddr = d.TopMovers[0].ResourceAddress
		g.primaryAddress = primaryAddr
		g.primaryNotes = d.TopMovers[0].Notes
	}

	for _, c := range d.Changes {
		// An empty primaryAddr means "no primary driver", in which
		// case we should not match changes that happen to have empty
		// addresses against it — every change is "other" in that case.
		if primaryAddr != "" && c.ResourceAddress == primaryAddr {
			continue
		}
		for _, n := range c.Notes {
			g.otherNotes = append(g.otherNotes, resourceNote{c.ResourceAddress, n})
		}
	}
	return g
}

// isEmpty reports whether the group has no notes at all. When true the
// prompt omits the entire caveats section.
func (g caveatGroup) isEmpty() bool {
	return len(g.primaryNotes) == 0 && len(g.otherNotes) == 0 && len(g.planWideNotes) == 0
}

// writeCaveatsByResource emits the three caveat sub-blocks (primary,
// other, plan-wide) into sb. Each sub-block is omitted independently
// when its slice is empty; the section as a whole is omitted when all
// three are empty.
func writeCaveatsByResource(sb *strings.Builder, d CostDiff) {
	g := buildCaveatGroup(d)
	if g.isEmpty() {
		return
	}

	if len(g.primaryNotes) > 0 {
		fmt.Fprintf(sb, "# Notable assumptions for the primary driver (%s)\n", g.primaryAddress)
		for _, n := range g.primaryNotes {
			sb.WriteString("- ")
			sb.WriteString(n)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	if len(g.otherNotes) > 0 {
		sb.WriteString("# Other notable assumptions (do NOT attribute to the primary driver)\n")
		for _, rn := range g.otherNotes {
			fmt.Fprintf(sb, "- %s: %s\n", rn.address, rn.note)
		}
		sb.WriteByte('\n')
	}

	if len(g.planWideNotes) > 0 {
		sb.WriteString("# Plan-wide notes\n")
		for _, n := range g.planWideNotes {
			sb.WriteString("- ")
			sb.WriteString(n)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}
}

package diff

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"text/template"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/pricing"
)

// MarkdownConfig customizes the Markdown rendering. The zero value is
// valid and produces the default presentation: full breakdown shown,
// caveats shown, the canonical comment marker, the project repo URL,
// and Analyze-supplied TopMovers.
//
// Polarity note on Hide* booleans. Spec called for "Show*" flags with
// a true default, but Go cannot distinguish an unset bool from an
// explicit `false`, so a Show* design would force every caller that
// constructs MarkdownConfig{} to also remember to set Show* = true. We
// inverted to Hide* so the zero value naturally means "show
// everything". Setting HideFullBreakdown / HideCaveats to true at the
// call site has the same effect a hypothetical Show*=false would.
type MarkdownConfig struct {
	CommentMarker     string
	RepoURL           string
	HideFullBreakdown bool
	HideCaveats       bool
	TopMoversCount    int
}

const (
	defaultCommentMarker = "cloudoracle-pr-v1"
	defaultRepoURL       = "https://github.com/Cro22/CloudOracle"

	// centavoTolerance is the threshold below which a delta is treated
	// as zero for sign-and-emoji purposes. Pricing deltas accumulate
	// floating-point noise — a "zero" change can land at $0.0000001 —
	// and a 🔴 emoji on a fraction-of-a-cent rounding error makes the
	// PR comment look broken to humans. Half a cent is well below
	// "anything a human cares about" without false-zeroing genuine
	// pennies.
	centavoTolerance = 0.005
)

// RenderMarkdown returns the CostDiff rendered as a self-contained
// Markdown comment suitable for posting on a GitHub pull request. Uses
// the default MarkdownConfig.
func RenderMarkdown(d CostDiff) string {
	return RenderMarkdownWithConfig(d, MarkdownConfig{})
}

// RenderMarkdownWithConfig is RenderMarkdown with explicit configuration.
// See MarkdownConfig for the supported knobs.
func RenderMarkdownWithConfig(d CostDiff, cfg MarkdownConfig) string {
	cfg = applyDefaults(cfg)
	data := buildTemplateData(d, cfg)
	var buf bytes.Buffer
	if err := mdTemplate.Execute(&buf, data); err != nil {
		// Should never trigger — the template is a hard-coded constant
		// vetted at init() via template.Must. If a future edit slips a
		// regression past tests, surface the failure inline instead of
		// silently emitting a half-rendered comment.
		return fmt.Sprintf("CloudOracle render error: %v", err)
	}
	return buf.String()
}

func applyDefaults(cfg MarkdownConfig) MarkdownConfig {
	if cfg.CommentMarker == "" {
		cfg.CommentMarker = defaultCommentMarker
	}
	if cfg.RepoURL == "" {
		cfg.RepoURL = defaultRepoURL
	}
	return cfg
}

// templateData is the precomputed shape passed to the Markdown
// template. Every dynamic value is preformatted here so the template
// body stays free of formatting logic — easier to reason about
// whitespace and easier to swap when milestone 15 introduces an
// LLM-generated narrative.
type templateData struct {
	NetChange           string
	TrendEmoji          string
	Narrative           string
	HasTopMovers        bool
	TopMovers           []tplRow
	ShowFullBreakdown   bool
	StatsPriced         int
	StatsSkippedTotal   int
	HasCreated          bool
	Created             []tplRow
	HasDeleted          bool
	Deleted             []tplRow
	HasUpdated          bool
	Updated             []tplRow
	HasReplaced         bool
	Replaced            []tplRow
	HasSkipped          bool
	Skipped             []tplSkipRow
	ShowCaveats         bool
	GlobalNotes         []string
	PerResourceNotes    []tplResourceNote
	AggregateConfidence string
	RepoURL             string
	CommentMarker       string
}

type tplRow struct {
	Address    string
	Action     string
	Delta      string
	Confidence string
	Breakdown  []tplLine
}

type tplLine struct {
	Component string
	Cost      string
}

type tplSkipRow struct {
	Address    string
	Type       string
	SkipReason string
}

type tplResourceNote struct {
	Note      string
	Addresses []string
}

func buildTemplateData(d CostDiff, cfg MarkdownConfig) templateData {
	data := templateData{
		NetChange:           formatDelta(d.TotalMonthlyDelta),
		TrendEmoji:          trendEmoji(d.TotalMonthlyDelta),
		Narrative:           renderNarrative(d),
		ShowFullBreakdown:   !cfg.HideFullBreakdown,
		ShowCaveats:         !cfg.HideCaveats,
		StatsPriced:         d.Stats.Priced,
		StatsSkippedTotal:   len(d.Skipped),
		AggregateConfidence: string(d.Confidence),
		RepoURL:             cfg.RepoURL,
		CommentMarker:       cfg.CommentMarker,
	}

	movers := selectTopMovers(d.TopMovers, cfg.TopMoversCount)
	for _, m := range movers {
		data.TopMovers = append(data.TopMovers, makeRow(m))
	}
	data.HasTopMovers = len(data.TopMovers) > 0

	for _, c := range d.Created {
		data.Created = append(data.Created, makeRow(c))
	}
	data.HasCreated = len(data.Created) > 0
	for _, c := range d.Deleted {
		data.Deleted = append(data.Deleted, makeRow(c))
	}
	data.HasDeleted = len(data.Deleted) > 0
	for _, c := range d.Updated {
		data.Updated = append(data.Updated, makeRow(c))
	}
	data.HasUpdated = len(data.Updated) > 0
	for _, c := range d.Replaced {
		data.Replaced = append(data.Replaced, makeRow(c))
	}
	data.HasReplaced = len(data.Replaced) > 0

	for _, c := range d.Skipped {
		data.Skipped = append(data.Skipped, tplSkipRow{
			Address:    c.ResourceAddress,
			Type:       c.ResourceType,
			SkipReason: c.SkipReason,
		})
	}
	data.HasSkipped = len(data.Skipped) > 0

	data.GlobalNotes = append([]string(nil), d.Notes...)
	data.PerResourceNotes = deduplicateNotes(d.Changes)

	return data
}

// selectTopMovers honours cfg.TopMoversCount: 0 (default) means "use
// what Analyze gave us", positive caps the count, negative selects
// nothing (a foot-gun guard rather than a meaningful feature).
func selectTopMovers(in []pricing.ChangeEstimate, n int) []pricing.ChangeEstimate {
	switch {
	case n < 0:
		return nil
	case n == 0:
		return in
	case n > len(in):
		return in
	default:
		return in[:n]
	}
}

func makeRow(c pricing.ChangeEstimate) tplRow {
	row := tplRow{
		Address:    c.ResourceAddress,
		Action:     actionDisplay(c.Action),
		Delta:      formatDelta(c.MonthlyDelta),
		Confidence: string(c.Confidence),
	}
	for _, li := range c.Breakdown {
		row.Breakdown = append(row.Breakdown, tplLine{
			Component: li.Component,
			Cost:      formatDelta(li.MonthlyUSD),
		})
	}
	return row
}

// deduplicateNotes groups identical Notes texts across resources so a
// caveat list shared by N resources renders as one bullet instead of N.
// The order of unique notes follows first-seen-in-Changes; addresses
// inside each note follow first-seen order; both matter for stable
// golden tests.
func deduplicateNotes(changes []pricing.ChangeEstimate) []tplResourceNote {
	byNote := map[string][]string{}
	var order []string
	for _, c := range changes {
		for _, n := range c.Notes {
			if _, ok := byNote[n]; !ok {
				order = append(order, n)
			}
			// Addresses are deduped too — repeating the same address
			// for the same note (which only happens via parser bugs)
			// would clutter the rendered list silently.
			addrs := byNote[n]
			seen := false
			for _, a := range addrs {
				if a == c.ResourceAddress {
					seen = true
					break
				}
			}
			if !seen {
				byNote[n] = append(addrs, c.ResourceAddress)
			}
		}
	}
	out := make([]tplResourceNote, 0, len(order))
	for _, n := range order {
		out = append(out, tplResourceNote{Note: n, Addresses: byNote[n]})
	}
	return out
}

// formatDelta renders a monetary value with explicit sign for a PR
// comment. Conventions:
//
//   - positive  → "+$60.74"
//   - negative  → "-$50.00"
//   - near-zero → "$0.00" (no sign — applied within centavoTolerance to
//     avoid surfacing floating-point noise as a real change)
//
// Always two decimals, always a dollar sign, never scientific notation.
func formatDelta(d float64) string {
	if math.Abs(d) < centavoTolerance {
		return "$0.00"
	}
	if d > 0 {
		return fmt.Sprintf("+$%.2f", d)
	}
	return fmt.Sprintf("-$%.2f", math.Abs(d))
}

// trendEmoji returns the colored circle that pairs with the net delta
// in the comment header. Within centavoTolerance the change is treated
// as zero (⚪) — a sub-cent fluctuation should not draw a red dot.
func trendEmoji(d float64) string {
	if math.Abs(d) < centavoTolerance {
		return "⚪"
	}
	if d > 0 {
		return "🔴"
	}
	return "🟢"
}

// actionDisplay returns the label shown in the Top movers table for an
// action. The emojis are deliberately verbose — a PR reviewer scanning
// a long comment recognises 🔄 replace much faster than the bare word.
// Unknown action strings pass through verbatim with no emoji rather
// than masking a parser issue with a generic icon.
func actionDisplay(a iac.Action) string {
	switch a {
	case iac.ActionCreate:
		return "🆕 create"
	case iac.ActionDelete:
		return "❌ delete"
	case iac.ActionUpdate:
		return "♻️ update"
	case iac.ActionReplace:
		return "🔄 replace"
	case iac.ActionNoop:
		return "⏭️ no-op"
	case iac.ActionRead:
		return "⏭️ read"
	}
	return string(a)
}

// renderNarrative produces the prose paragraph between the header and
// the top-movers table. This is the function milestone 15 will replace
// with an LLM call: the contract is "input CostDiff, output 1-3
// sentences in Markdown, no headings or lists".
//
// Today's templated logic walks the action stats and the delta sign to
// build a single sentence, optionally appended with an estimation-
// failure count when relevant.
func renderNarrative(d CostDiff) string {
	if d.Stats.Priced == 0 {
		return "No priceable resources in this plan."
	}

	var verbs []string
	if d.Stats.Created > 0 {
		verbs = append(verbs, fmt.Sprintf("adds %d %s", d.Stats.Created, plural("resource", d.Stats.Created)))
	}
	if d.Stats.Deleted > 0 {
		verbs = append(verbs, fmt.Sprintf("removes %d", d.Stats.Deleted))
	}
	if d.Stats.Updated > 0 {
		verbs = append(verbs, fmt.Sprintf("updates %d", d.Stats.Updated))
	}
	if d.Stats.Replaced > 0 {
		verbs = append(verbs, fmt.Sprintf("replaces %d", d.Stats.Replaced))
	}
	actions := joinClauses(verbs)

	var direction string
	switch {
	case math.Abs(d.TotalMonthlyDelta) < centavoTolerance:
		direction = "no net cost change"
	case d.TotalMonthlyDelta > 0:
		direction = "a net monthly cost increase of " + formatDelta(d.TotalMonthlyDelta)
	default:
		direction = "a net monthly cost decrease of " + formatDelta(d.TotalMonthlyDelta)
	}

	sentence := fmt.Sprintf("This plan %s, with %s.", actions, direction)

	estFail := 0
	for _, s := range d.Skipped {
		if strings.Contains(s.SkipReason, "estimation failed") {
			estFail++
		}
	}
	if estFail > 0 {
		sentence += fmt.Sprintf(" %d %s could not be priced.", estFail, plural("resource", estFail))
	}

	return sentence
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// joinClauses produces an Oxford-comma list. Empty input is an empty
// string; one element returns itself; two are joined by " and "; three
// or more by ", " with " and " before the last.
func joinClauses(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// addressList formats a list of resource addresses inline as
// `addr1`, `addr2`, ... — used inside the per-resource caveat bullets.
func addressList(addrs []string) string {
	var sb strings.Builder
	for i, a := range addrs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('`')
		sb.WriteString(a)
		sb.WriteByte('`')
	}
	return sb.String()
}

var mdTemplate = template.Must(template.New("md").Funcs(template.FuncMap{
	"addressList": addressList,
}).Parse(markdownTemplate))

// markdownTemplate is the canonical layout for a CostDiff rendered as a
// PR comment. Whitespace is fiddly: blank lines are needed between
// Markdown sections (otherwise GitHub merges paragraphs), and trim
// markers on `{{- }}` actions are used to keep the template readable
// without injecting spurious indentation. If you change this, run
// `go test -update ./internal/diff/...` to refresh the goldens and
// eyeball the diff.
const markdownTemplate = "## 💰 Cloud Cost Impact\n" +
	"\n" +
	"**Net monthly change: {{.NetChange}}** {{.TrendEmoji}}\n" +
	"\n" +
	"{{.Narrative}}\n" +
	"{{- if .HasTopMovers}}\n" +
	"\n" +
	"### Top movers by cost impact\n" +
	"\n" +
	"| Resource | Action | Δ Monthly | Confidence |\n" +
	"|----------|--------|-----------|------------|\n" +
	"{{- range .TopMovers}}\n" +
	"| `{{.Address}}` | {{.Action}} | {{.Delta}} | {{.Confidence}} |\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- if .ShowFullBreakdown}}\n" +
	"\n" +
	"<details>\n" +
	"<summary>📋 Full breakdown ({{.StatsPriced}} priced, {{.StatsSkippedTotal}} skipped)</summary>\n" +
	"{{- if .HasCreated}}\n" +
	"\n" +
	"#### Created ({{len .Created}})\n" +
	"{{range .Created}}\n" +
	"- `{{.Address}}` — {{.Delta}}\n" +
	"{{- range .Breakdown}}\n" +
	"  - {{.Component}}: {{.Cost}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- if .HasDeleted}}\n" +
	"\n" +
	"#### Deleted ({{len .Deleted}})\n" +
	"{{range .Deleted}}\n" +
	"- `{{.Address}}` — {{.Delta}}\n" +
	"{{- range .Breakdown}}\n" +
	"  - {{.Component}}: {{.Cost}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- if .HasUpdated}}\n" +
	"\n" +
	"#### Updated ({{len .Updated}})\n" +
	"{{range .Updated}}\n" +
	"- `{{.Address}}` — {{.Delta}}\n" +
	"{{- range .Breakdown}}\n" +
	"  - {{.Component}}: {{.Cost}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- if .HasReplaced}}\n" +
	"\n" +
	"#### Replaced ({{len .Replaced}})\n" +
	"{{range .Replaced}}\n" +
	"- `{{.Address}}` — {{.Delta}}\n" +
	"{{- range .Breakdown}}\n" +
	"  - {{.Component}}: {{.Cost}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"{{- if .HasSkipped}}\n" +
	"\n" +
	"#### Skipped ({{len .Skipped}})\n" +
	"{{range .Skipped}}\n" +
	"- `{{.Address}}` ({{.Type}}) — {{.SkipReason}}\n" +
	"{{- end}}\n" +
	"{{- end}}\n" +
	"\n" +
	"</details>\n" +
	"{{- end}}\n" +
	"{{- if .ShowCaveats}}\n" +
	"\n" +
	"<details>\n" +
	"<summary>⚠️ Assumptions and caveats</summary>\n" +
	"\n" +
	"{{range .GlobalNotes}}- {{.}}\n{{end}}" +
	"{{range .PerResourceNotes}}- {{.Note}} _(applies to: {{addressList .Addresses}})_\n{{end}}" +
	"\n" +
	"</details>\n" +
	"{{- end}}\n" +
	"\n" +
	"---\n" +
	"\n" +
	"<sub>Generated by [CloudOracle]({{.RepoURL}}) · Confidence: **{{.AggregateConfidence}}**</sub>\n" +
	"\n" +
	"<!-- {{.CommentMarker}} -->\n"

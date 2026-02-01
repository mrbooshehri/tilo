package color

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Rule struct {
	Name    string
	Regex   *regexp.Regexp
	Color   string
	Style   string
	Enabled bool
}

var ansiColors = map[string]string{
	"black":   "30",
	"red":     "31",
	"green":   "32",
	"yellow":  "33",
	"blue":    "34",
	"magenta": "35",
	"cyan":    "36",
	"white":   "37",
	"gray":    "90",
}

var ansiStyles = map[string]string{
	"bold":      "1",
	"dim":       "2",
	"underline": "4",
}

const reset = "\x1b[0m"

func Wrap(text, colorName, style string) string {
	code := colorCode(colorName, style)
	if code == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + reset
}

func colorCode(colorName, style string) string {
	colorName = strings.ToLower(colorName)
	style = strings.ToLower(style)
	var parts []string
	if style != "" {
		if s, ok := ansiStyles[style]; ok {
			parts = append(parts, s)
		}
	}
	if colorName != "" {
		if c, ok := ansiColors[colorName]; ok {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, ";")
}

func ApplyRules(line string, rules []Rule) string {
	if len(rules) == 0 || line == "" {
		return line
	}
	type span struct {
		start int
		end   int
		color string
		style string
	}
	occupied := make([]bool, len(line))
	var spans []span
	for _, rule := range rules {
		if !rule.Enabled || rule.Regex == nil {
			continue
		}
		indices := rule.Regex.FindAllStringIndex(line, -1)
		for _, idx := range indices {
			start, end := idx[0], idx[1]
			if start >= end {
				continue
			}
			skip := false
			for i := start; i < end; i++ {
				if occupied[i] {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			for i := start; i < end; i++ {
				occupied[i] = true
			}
			spans = append(spans, span{
				start: start,
				end:   end,
				color: rule.Color,
				style: rule.Style,
			})
		}
	}
	if len(spans) == 0 {
		return line
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	var out strings.Builder
	pos := 0
	for _, sp := range spans {
		if sp.start < pos {
			continue
		}
		out.WriteString(line[pos:sp.start])
		out.WriteString(Wrap(line[sp.start:sp.end], sp.color, sp.style))
		pos = sp.end
	}
	out.WriteString(line[pos:])
	return out.String()
}

func BuildDefaultRules() []Rule {
	return []Rule{
		{
			Name:  "timestamp",
			Color: "cyan",
			Regex: regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?\b|\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\b`),
			Enabled: true,
		},
		{
			Name:    "url",
			Color:   "blue",
			Regex:   regexp.MustCompile(`\bhttps?://[^\s\)\]\}\>\,\;\:]+`),
			Enabled: true,
		},
		{
			Name:    "ipv4",
			Color:   "yellow",
			Regex:   regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
			Enabled: true,
		},
		{
			Name:    "ipv6",
			Color:   "yellow",
			Regex:   regexp.MustCompile(`\b(?:[0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}\b`),
			Enabled: true,
		},
		{
			Name:    "mac",
			Color:   "yellow",
			Regex:   regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}\b`),
			Enabled: true,
		},
		{
			Name:    "port",
			Color:   "magenta",
			Regex:   regexp.MustCompile(`:\d{2,5}\b`),
			Enabled: true,
		},
		{
			Name:    "path",
			Color:   "green",
			Regex:   regexp.MustCompile(`\B/(?:[^\s\)\]\}\>\,\;\:]+)`),
			Enabled: true,
		},
		{
			Name:    "level_error",
			Color:   "red",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\b(ERROR|FATAL)\b`),
			Enabled: true,
		},
		{
			Name:    "level_warn",
			Color:   "yellow",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\b(WARN|WARNING)\b`),
			Enabled: true,
		},
		{
			Name:    "level_info",
			Color:   "blue",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\bINFO\b`),
			Enabled: true,
		},
		{
			Name:    "level_debug",
			Color:   "magenta",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\bDEBUG\b`),
			Enabled: true,
		},
		{
			Name:    "level_trace",
			Color:   "gray",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\bTRACE\b`),
			Enabled: true,
		},
		{
			Name:    "fail",
			Color:   "red",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\b(fail|failed|failure|error|err|fatal|panic|crashed|crash|abort|aborted|timeout|timedout|refused|reject|denied|unreachable|unavailable|corrupted|invalid)\b`),
			Enabled: true,
		},
		{
			Name:    "success",
			Color:   "green",
			Style:   "bold",
			Regex:   regexp.MustCompile(`(?i)\b(ok|okay|success|successful|successfully|succeeded|complete|completed|done|ready|healthy|passed|pass|connected|accepted|resolved)\b`),
			Enabled: true,
		},
		{
			Name:    "keyword",
			Color:   "magenta",
			Regex:   regexp.MustCompile(`(?i)\b(kube|pod|node|container|nginx|envoy|http|grpc|tcp|udp|timeout|retry|panic|crash)\b`),
			Enabled: true,
		},
	}
}

func BuildRules(defaults []Rule, overrides map[string]string, disable []string, custom []CustomRule) ([]Rule, error) {
	disabled := map[string]bool{}
	for _, name := range disable {
		disabled[strings.ToLower(name)] = true
	}

	var rules []Rule
	for _, rule := range defaults {
		rule.Enabled = !disabled[strings.ToLower(rule.Name)]
		if colorOverride, ok := overrides[strings.ToLower(rule.Name)]; ok {
			rule.Color = colorOverride
		}
		rules = append(rules, rule)
	}

	for _, customRule := range custom {
		r, err := customRule.toRule()
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}

	return rules, nil
}

type CustomRule struct {
	Pattern string
	Color   string
	Style   string
}

func (r CustomRule) toRule() (Rule, error) {
	re, err := regexp.Compile(r.Pattern)
	if err != nil {
		return Rule{}, fmt.Errorf("invalid custom rule regex %q: %w", r.Pattern, err)
	}
	return Rule{
		Name:    "custom",
		Regex:   re,
		Color:   r.Color,
		Style:   r.Style,
		Enabled: true,
	}, nil
}

func HighlightQuery(line, query string) string {
	if query == "" {
		return line
	}
	lowerLine := strings.ToLower(line)
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lowerLine, lowerQuery)
	if idx == -1 {
		return line
	}
	var out strings.Builder
	start := 0
	for idx != -1 {
		out.WriteString(line[start:idx])
		match := line[idx : idx+len(query)]
		out.WriteString(Wrap(match, "blue", "underline"))
		start = idx + len(query)
		next := strings.Index(lowerLine[start:], lowerQuery)
		if next == -1 {
			break
		}
		idx = start + next
	}
	out.WriteString(line[start:])
	return out.String()
}

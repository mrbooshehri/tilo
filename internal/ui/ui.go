package ui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"golang.org/x/term"

	"tilo/internal/color"
)

const (
	clearScreen = "\x1b[2J"
	moveHome    = "\x1b[H"
	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
	reverseOn   = "\x1b[7m"
	reverseOff  = "\x1b[27m"
)

type Viewer struct {
	Lines       []string
	Rules       []color.Rule
	Plain       bool
	Cursor      int
	Top         int
	Query       string
	Matches     []int
	MatchIndex  int
	SelectStart *int
	Status      string
	StatusAtTop bool
}

func Run(lines []string, rules []color.Rule, plain bool, statusAtTop bool) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive mode requires a terminal")
	}

	viewer := &Viewer{
		Lines:       lines,
		Rules:       rules,
		Plain:       plain,
		StatusAtTop: statusAtTop,
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)

	fmt.Fprint(os.Stdout, hideCursor)
	defer fmt.Fprint(os.Stdout, showCursor)

	reader := bufio.NewReader(os.Stdin)
	for {
		viewer.draw()
		b, err := reader.ReadByte()
		if err != nil {
			return err
		}
		switch b {
		case 'q':
			return nil
		case 'j':
			viewer.moveCursor(1)
		case 'k':
			viewer.moveCursor(-1)
		case 'g':
			viewer.cursorTop()
		case 'G':
			viewer.cursorBottom()
		case '/':
			query := viewer.prompt(reader, "/")
			viewer.setQuery(query)
		case 'n':
			viewer.nextMatch(1)
		case 'N':
			viewer.nextMatch(-1)
		case 'v':
			viewer.toggleSelect()
		case 'y':
			viewer.copySelection()
		case 0x1b:
			viewer.handleEscape(reader)
		}
	}
}

func (v *Viewer) draw() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width, height = 80, 24
	}
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	v.clampCursor()
	v.ensureVisible(contentHeight)

	fmt.Fprint(os.Stdout, moveHome)
	fmt.Fprint(os.Stdout, clearScreen)
	if v.StatusAtTop {
		fmt.Fprint(os.Stdout, v.statusLine(width))
		fmt.Fprint(os.Stdout, "\r\n")
	}
	start := v.Top
	end := v.Top + contentHeight
	if end > len(v.Lines) {
		end = len(v.Lines)
	}

	for i := start; i < end; i++ {
		line := v.Lines[i]
		if !v.Plain {
			line = color.ApplyRules(line, v.Rules)
			line = color.HighlightQuery(line, v.Query)
		}
		if v.isSelected(i) {
			line = reverseOn + line + reverseOff
		}
		fmt.Fprint(os.Stdout, truncateANSI(line, width))
		fmt.Fprint(os.Stdout, "\r\n")
	}

	if !v.StatusAtTop {
		fmt.Fprint(os.Stdout, v.statusLine(width))
	}
}

func (v *Viewer) statusLine(width int) string {
	lineInfo := fmt.Sprintf("%d/%d", v.Cursor+1, len(v.Lines))
	matchInfo := ""
	if v.Query != "" && len(v.Matches) > 0 {
		matchInfo = fmt.Sprintf(" | match %d/%d", v.MatchIndex+1, len(v.Matches))
	}
	selection := ""
	if v.SelectStart != nil {
		selection = " | visual"
	}
	status := fmt.Sprintf("%s%s%s", lineInfo, matchInfo, selection)
	if v.Query != "" {
		status += " | /" + v.Query
	}
	if v.Status != "" {
		status += " | " + v.Status
	}
	help := "q quit • / search • n/N next • j/k move • g/G top/bot • v select • y yank"
	full := fmt.Sprintf("%s | %s", status, help)
	return padRight(full, width)
}

func (v *Viewer) prompt(reader *bufio.Reader, prefix string) string {
	v.Status = ""
	fmt.Fprint(os.Stdout, moveHome)
	width, height, _ := term.GetSize(int(os.Stdout.Fd()))
	if v.StatusAtTop {
		fmt.Fprint(os.Stdout, "\x1b[1;1H")
	} else {
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H", height)
	}
	fmt.Fprint(os.Stdout, padRight(prefix, width))
	if v.StatusAtTop {
		fmt.Fprint(os.Stdout, "\x1b[1;1H")
	} else {
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H", height)
	}
	fmt.Fprint(os.Stdout, prefix)

	var buf []rune
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return string(buf)
		}
		switch b {
		case '\r', '\n':
			return string(buf)
		case 0x7f, 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(os.Stdout, "\b \b")
			}
		default:
			if b < 32 {
				continue
			}
			r, _ := utf8.DecodeRune([]byte{b})
			buf = append(buf, r)
			fmt.Fprint(os.Stdout, string(r))
		}
	}
}

func (v *Viewer) handleEscape(reader *bufio.Reader) {
	b, err := reader.ReadByte()
	if err != nil {
		return
	}
	if b != '[' {
		return
	}
	code, err := reader.ReadByte()
	if err != nil {
		return
	}
	switch code {
	case 'A':
		v.moveCursor(-1)
	case 'B':
		v.moveCursor(1)
	case '5':
		_, _ = reader.ReadByte()
		v.page(-1)
	case '6':
		_, _ = reader.ReadByte()
		v.page(1)
	}
}

func (v *Viewer) moveCursor(delta int) {
	v.Cursor += delta
	v.clampCursor()
	v.Status = ""
}

func (v *Viewer) page(delta int) {
	_, height, _ := term.GetSize(int(os.Stdout.Fd()))
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	v.Cursor += delta * contentHeight
	v.clampCursor()
}

func (v *Viewer) clampCursor() {
	if v.Cursor < 0 {
		v.Cursor = 0
	}
	if v.Cursor >= len(v.Lines) {
		v.Cursor = len(v.Lines) - 1
	}
	if len(v.Lines) == 0 {
		v.Cursor = 0
	}
}

func (v *Viewer) ensureVisible(height int) {
	if v.Cursor < v.Top {
		v.Top = v.Cursor
	}
	if v.Cursor >= v.Top+height {
		v.Top = v.Cursor - height + 1
	}
	if v.Top < 0 {
		v.Top = 0
	}
	maxTop := len(v.Lines) - height
	if maxTop < 0 {
		maxTop = 0
	}
	if v.Top > maxTop {
		v.Top = maxTop
	}
}

func (v *Viewer) cursorTop() {
	v.Cursor = 0
	v.Status = ""
}

func (v *Viewer) cursorBottom() {
	if len(v.Lines) == 0 {
		v.Cursor = 0
		return
	}
	v.Cursor = len(v.Lines) - 1
	v.Status = ""
}

func (v *Viewer) setQuery(query string) {
	v.Query = strings.TrimSpace(query)
	v.Matches = nil
	v.MatchIndex = 0
	if v.Query == "" {
		return
	}
	lowerQuery := strings.ToLower(v.Query)
	for i, line := range v.Lines {
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			v.Matches = append(v.Matches, i)
		}
	}
	if len(v.Matches) == 0 {
		v.Status = "no matches"
		return
	}
	v.MatchIndex = 0
	v.Cursor = v.Matches[0]
	v.Status = ""
}

func (v *Viewer) nextMatch(dir int) {
	if len(v.Matches) == 0 {
		v.Status = "no matches"
		return
	}
	v.MatchIndex += dir
	if v.MatchIndex < 0 {
		v.MatchIndex = len(v.Matches) - 1
	}
	if v.MatchIndex >= len(v.Matches) {
		v.MatchIndex = 0
	}
	v.Cursor = v.Matches[v.MatchIndex]
	v.Status = ""
}

func (v *Viewer) toggleSelect() {
	if v.SelectStart == nil {
		idx := v.Cursor
		v.SelectStart = &idx
		v.Status = "select start"
		return
	}
	v.SelectStart = nil
	v.Status = "selection cleared"
}

func (v *Viewer) isSelected(idx int) bool {
	if v.SelectStart == nil {
		return false
	}
	start := *v.SelectStart
	if start <= idx && idx <= v.Cursor {
		return true
	}
	if v.Cursor <= idx && idx <= start {
		return true
	}
	return false
}

func (v *Viewer) copySelection() {
	if v.SelectStart == nil {
		v.Status = "no selection"
		return
	}
	start := *v.SelectStart
	end := v.Cursor
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end >= len(v.Lines) {
		end = len(v.Lines) - 1
	}
	if start > end {
		v.Status = "no selection"
		return
	}
	text := strings.Join(v.Lines[start:end+1], "\n")
	if err := clipboard.WriteAll(text); err != nil {
		v.Status = "clipboard failed"
		return
	}
	v.Status = fmt.Sprintf("copied %d lines", end-start+1)
}

func padRight(s string, width int) string {
	if width <= 0 {
		return s
	}
	if len(stripANSI(s)) >= width {
		return truncateANSI(s, width)
	}
	return s + strings.Repeat(" ", width-len(stripANSI(s)))
}

func truncateANSI(s string, width int) string {
	if width <= 0 {
		return ""
	}
	plain := stripANSI(s)
	if len(plain) <= width {
		return s
	}
	var out strings.Builder
	count := 0
	inEscape := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\x1b' {
			inEscape = true
		}
		if !inEscape {
			if count >= width {
				break
			}
			count++
		}
		out.WriteByte(ch)
		if inEscape && ch == 'm' {
			inEscape = false
		}
	}
	out.WriteString("\x1b[0m")
	return out.String()
}

func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if ch == 'm' {
				inEscape = false
			}
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

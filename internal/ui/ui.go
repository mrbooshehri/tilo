package ui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"golang.org/x/term"

	"tilo/internal/color"
)

const (
	moveHome    = "\x1b[H"
	showCursor  = "\x1b[?25h"
	reverseOn   = "\x1b[7m"
	reverseOff  = "\x1b[27m"
	statusBG    = "\x1b[100m"
	statusFG    = "\x1b[97m"
	resetStyle  = "\x1b[0m"
)

type Viewer struct {
	Lines       []string
	Rules       []color.Rule
	Plain       bool
	Cursor      int
	CursorCol   int
	Top         int
	Query       string
	Matches     []int
	MatchIndex  int
	SelectStart *int
	Status      string
	StatusAtTop bool
	LineNumbers bool
}

func Run(lines []string, rules []color.Rule, plain bool, statusAtTop bool, lineNumbers bool) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive mode requires a terminal")
	}

	viewer := &Viewer{
		Lines:       lines,
		Rules:       rules,
		Plain:       plain,
		StatusAtTop: statusAtTop,
		LineNumbers: lineNumbers,
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)

	fmt.Fprint(os.Stdout, showCursor)
	defer func() {
		fmt.Fprint(os.Stdout, resetStyle)
		fmt.Fprint(os.Stdout, moveHome)
		fmt.Fprint(os.Stdout, "\x1b[2J")
	}()

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
		case 'h':
			viewer.moveCursorCol(-1)
		case 'l':
			viewer.moveCursorCol(1)
		case '0':
			viewer.moveLineStart()
		case '$':
			viewer.moveLineEnd()
		case 'w':
			viewer.moveWordForward()
		case 'b':
			viewer.moveWordBackward()
		case 'e':
			viewer.moveWordEnd()
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
		case 'L':
			viewer.LineNumbers = !viewer.LineNumbers
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
	if v.StatusAtTop {
		fmt.Fprint(os.Stdout, statusBG+statusFG)
		fmt.Fprint(os.Stdout, v.statusLine(width))
		fmt.Fprint(os.Stdout, resetStyle)
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
		if v.LineNumbers {
			prefix := fmt.Sprintf("%*d ", v.lineNumberWidth(), i+1)
			line = prefix + line
		}
		if v.isSelected(i) {
			line = reverseOn + line + reverseOff
		}
		fmt.Fprint(os.Stdout, padRight(truncateANSI(line, width), width))
		fmt.Fprint(os.Stdout, "\r\n")
	}
	for i := end; i < start+contentHeight; i++ {
		fmt.Fprint(os.Stdout, strings.Repeat(" ", width))
		fmt.Fprint(os.Stdout, "\r\n")
	}

	if !v.StatusAtTop {
		fmt.Fprint(os.Stdout, statusBG+statusFG)
		fmt.Fprint(os.Stdout, v.statusLine(width))
		fmt.Fprint(os.Stdout, resetStyle)
	}

	v.moveCursorToLine()
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
	help := "q quit • / search • n/N next • h/j/k/l move • w/b/e word • 0/$ line • g/G top/bot • v select • y yank • L line#"
	full := fmt.Sprintf("%s | %s", status, help)
	return padRight(full, width)
}

func (v *Viewer) moveCursorToLine() {
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		height = 24
	}
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	row := v.Cursor - v.Top
	if row < 0 {
		row = 0
	}
	if row >= contentHeight {
		row = contentHeight - 1
	}
	if v.StatusAtTop {
		row++
	}
	row++
	col := 1 + v.CursorCol
	if v.LineNumbers {
		col = v.lineNumberWidth() + 2 + v.CursorCol
	}
	fmt.Fprintf(os.Stdout, "\x1b[%d;%dH", row, col)
}

func (v *Viewer) lineNumberWidth() int {
	if len(v.Lines) == 0 {
		return 1
	}
	return len(fmt.Sprintf("%d", len(v.Lines)))
}

func (v *Viewer) lineRuneCount(idx int) int {
	if idx < 0 || idx >= len(v.Lines) {
		return 0
	}
	return utf8.RuneCountInString(v.Lines[idx])
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
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
	case 'C':
		v.moveCursorCol(1)
	case 'D':
		v.moveCursorCol(-1)
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

func (v *Viewer) moveCursorCol(delta int) {
	v.CursorCol += delta
	v.clampCursor()
	v.Status = ""
}

func (v *Viewer) moveLineStart() {
	v.CursorCol = 0
	v.clampCursor()
	v.Status = ""
}

func (v *Viewer) moveLineEnd() {
	maxCol := v.lineRuneCount(v.Cursor)
	if maxCol > 0 {
		maxCol--
	}
	v.CursorCol = maxCol
	v.clampCursor()
	v.Status = ""
}

func (v *Viewer) moveWordForward() {
	if len(v.Lines) == 0 {
		return
	}
	lineIdx := v.Cursor
	col := v.CursorCol
	for {
		line := []rune(v.Lines[lineIdx])
		if len(line) == 0 {
			if lineIdx+1 >= len(v.Lines) {
				v.Cursor = lineIdx
				v.CursorCol = 0
				v.clampCursor()
				v.Status = ""
				return
			}
			lineIdx++
			col = 0
			continue
		}
		if col < 0 {
			col = 0
		}
		if col >= len(line) {
			if lineIdx+1 >= len(v.Lines) {
				v.Cursor = lineIdx
				v.CursorCol = len(line) - 1
				v.clampCursor()
				v.Status = ""
				return
			}
			lineIdx++
			col = 0
			continue
		}
		pos := col
		if isWordRune(line[pos]) {
			for pos < len(line) && isWordRune(line[pos]) {
				pos++
			}
		}
		for pos < len(line) && !isWordRune(line[pos]) {
			pos++
		}
		if pos < len(line) {
			v.Cursor = lineIdx
			v.CursorCol = pos
			v.clampCursor()
			v.Status = ""
			return
		}
		if lineIdx+1 >= len(v.Lines) {
			v.Cursor = lineIdx
			v.CursorCol = len(line) - 1
			v.clampCursor()
			v.Status = ""
			return
		}
		lineIdx++
		col = 0
	}
}

func (v *Viewer) moveWordBackward() {
	if len(v.Lines) == 0 {
		return
	}
	lineIdx := v.Cursor
	col := v.CursorCol
	for {
		line := []rune(v.Lines[lineIdx])
		if len(line) == 0 {
			if lineIdx == 0 {
				v.Cursor = 0
				v.CursorCol = 0
				v.clampCursor()
				v.Status = ""
				return
			}
			lineIdx--
			prev := []rune(v.Lines[lineIdx])
			if len(prev) == 0 {
				col = 0
			} else {
				col = len(prev) - 1
			}
			continue
		}
		if col >= len(line) {
			col = len(line) - 1
		}
		if col < 0 {
			col = 0
		}
		pos := col
		if isWordRune(line[pos]) {
			for pos > 0 && isWordRune(line[pos-1]) {
				pos--
			}
			v.Cursor = lineIdx
			v.CursorCol = pos
			v.clampCursor()
			v.Status = ""
			return
		}
		for pos > 0 && !isWordRune(line[pos]) {
			pos--
		}
		if isWordRune(line[pos]) {
			for pos > 0 && isWordRune(line[pos-1]) {
				pos--
			}
			v.Cursor = lineIdx
			v.CursorCol = pos
			v.clampCursor()
			v.Status = ""
			return
		}
		if lineIdx == 0 {
			v.Cursor = 0
			v.CursorCol = 0
			v.clampCursor()
			v.Status = ""
			return
		}
		lineIdx--
		prev := []rune(v.Lines[lineIdx])
		if len(prev) == 0 {
			col = 0
		} else {
			col = len(prev) - 1
		}
	}
}

func (v *Viewer) moveWordEnd() {
	if len(v.Lines) == 0 {
		return
	}
	lineIdx := v.Cursor
	col := v.CursorCol
	for {
		line := []rune(v.Lines[lineIdx])
		if len(line) == 0 {
			if lineIdx+1 >= len(v.Lines) {
				v.Cursor = lineIdx
				v.CursorCol = 0
				v.clampCursor()
				v.Status = ""
				return
			}
			lineIdx++
			col = 0
			continue
		}
		if col < 0 {
			col = 0
		}
		if col >= len(line) {
			if lineIdx+1 >= len(v.Lines) {
				v.Cursor = lineIdx
				v.CursorCol = len(line) - 1
				v.clampCursor()
				v.Status = ""
				return
			}
			lineIdx++
			col = 0
			continue
		}
		pos := col
		if isWordRune(line[pos]) {
			for pos < len(line) && isWordRune(line[pos]) {
				pos++
			}
			v.Cursor = lineIdx
			v.CursorCol = pos - 1
			v.clampCursor()
			v.Status = ""
			return
		}
		for pos < len(line) && !isWordRune(line[pos]) {
			pos++
		}
		if pos < len(line) {
			for pos < len(line) && isWordRune(line[pos]) {
				pos++
			}
			v.Cursor = lineIdx
			v.CursorCol = pos - 1
			v.clampCursor()
			v.Status = ""
			return
		}
		if lineIdx+1 >= len(v.Lines) {
			v.Cursor = lineIdx
			v.CursorCol = len(line) - 1
			v.clampCursor()
			v.Status = ""
			return
		}
		lineIdx++
		col = 0
	}
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
	maxCol := v.lineRuneCount(v.Cursor)
	if maxCol > 0 {
		maxCol--
	}
	if v.CursorCol < 0 {
		v.CursorCol = 0
	}
	if v.CursorCol > maxCol {
		v.CursorCol = maxCol
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
	v.CursorCol = 0
	v.Status = ""
}

func (v *Viewer) cursorBottom() {
	if len(v.Lines) == 0 {
		v.Cursor = 0
		v.CursorCol = 0
		return
	}
	v.Cursor = len(v.Lines) - 1
	v.CursorCol = 0
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
	v.CursorCol = 0
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
	v.CursorCol = 0
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

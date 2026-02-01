package ui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"golang.org/x/term"

	"tilo/internal/color"
)

const (
	moveHome    = "\x1b[H"
	hideCursor  = "\x1b[?25l"
	showCursor  = "\x1b[?25h"
	reverseOn   = "\x1b[7m"
	reverseOff  = "\x1b[27m"
	statusBG    = "\x1b[100m"
	statusFG    = "\x1b[97m"
	resetStyle  = "\x1b[0m"
	cursorBlock = "\x1b[2 q"
	cursorReset = "\x1b[0 q"
	enterAlt    = "\x1b[?1049h"
	exitAlt     = "\x1b[?1049l"
)

type Viewer struct {
	Lines       []string
	Rules       []color.Rule
	Plain       bool
	Cursor      int
	CursorCol   int
	GoalCol     int
	Top         int
	TopSub      int
	Query       string
	Matches     []int
	MatchIndex  int
	SelectStart *Position
	SelectMode  SelectionMode
	Status      string
	StatusAtTop bool
	LineNumbers bool
	Wrap        bool
	HOffset     int
	Follow      bool
	InPrompt    bool
}

type Position struct {
	Line int
	Col  int
}

type SelectionMode int

const (
	SelectNone SelectionMode = iota
	SelectChar
	SelectLine
	SelectBlock
)

type segment struct {
	start int
	end   int
}

func Run(lines []string, rules []color.Rule, plain bool, statusAtTop bool, lineNumbers bool, follow bool, followCh <-chan []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive mode requires a terminal")
	}

	viewer := &Viewer{
		Lines:       lines,
		Rules:       rules,
		Plain:       plain,
		StatusAtTop: statusAtTop,
		LineNumbers: lineNumbers,
		Follow:      follow,
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	fd := int(os.Stdin.Fd())
	nonblock := follow || followCh != nil
	if nonblock {
		if err := syscall.SetNonblock(fd, true); err != nil {
			return err
		}
		defer func() {
			_ = syscall.SetNonblock(fd, false)
		}()
	}
	setNonblock := func(enable bool) {
		if nonblock {
			_ = syscall.SetNonblock(fd, enable)
		}
	}

	fmt.Fprint(os.Stdout, enterAlt)
	fmt.Fprint(os.Stdout, showCursor)
	fmt.Fprint(os.Stdout, cursorBlock)
	defer func() {
		fmt.Fprint(os.Stdout, cursorReset)
		fmt.Fprint(os.Stdout, resetStyle)
		fmt.Fprint(os.Stdout, exitAlt)
	}()

	reader := bufio.NewReader(os.Stdin)
	dirty := true
	for {
		if dirty {
			viewer.draw()
			dirty = false
		}
		b, err := reader.ReadByte()
		if err != nil {
			if nonblock && (errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)) {
				if followCh != nil {
					select {
					case batch, ok := <-followCh:
						if ok {
							viewer.appendLines(batch)
							dirty = true
						} else {
							followCh = nil
						}
					default:
						time.Sleep(30 * time.Millisecond)
					}
				} else {
					time.Sleep(30 * time.Millisecond)
				}
				continue
			}
			return err
		}
		switch b {
		case '\r', '\n':
			if viewer.Follow {
				viewer.appendLines([]string{""})
			}
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
		case 'I':
			viewer.moveLineStart()
		case '$':
			viewer.moveLineEnd()
		case 'A':
			viewer.moveLineEnd()
		case 'w':
			viewer.moveWordForward()
		case 'b':
			viewer.moveWordBackward()
		case 'e':
			viewer.moveWordEnd()
		case 'W':
			viewer.toggleWrap()
		case 'g':
			viewer.cursorTop()
		case 'G':
			viewer.cursorBottom()
		case '/':
			setNonblock(false)
			query, canceled := viewer.prompt(reader, "/")
			setNonblock(true)
			if !canceled {
				viewer.setQuery(query, 1)
			}
		case '?':
			setNonblock(false)
			query, canceled := viewer.prompt(reader, "?")
			setNonblock(true)
			if !canceled {
				viewer.setQuery(query, -1)
			}
		case 'n':
			viewer.nextMatch(1)
		case 'N':
			viewer.nextMatch(-1)
		case 'v':
			viewer.toggleSelect(SelectChar)
		case 'V':
			viewer.toggleSelect(SelectLine)
		case 'y':
			viewer.copySelection()
		case 'L':
			viewer.LineNumbers = !viewer.LineNumbers
		case 0x1b:
			if viewer.SelectMode != SelectNone {
				viewer.clearSelection()
			} else {
				viewer.handleEscape(reader)
			}
		case 0x16:
			viewer.toggleSelect(SelectBlock)
		}
		dirty = true
	}
}

func (v *Viewer) draw() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width, height = 80, 24
	}
	fmt.Fprint(os.Stdout, hideCursor)
	contentHeight := height - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	contentWidth := v.contentWidth(width)
	v.clampCursor()
	v.ensureVisible(contentHeight, contentWidth)

	fmt.Fprint(os.Stdout, moveHome)
	if v.StatusAtTop {
		fmt.Fprint(os.Stdout, v.renderStatusLine(width))
		fmt.Fprint(os.Stdout, "\r\n")
	}
	row := 0
	lineIdx := v.Top
	sub := v.TopSub
	for row < contentHeight && lineIdx < len(v.Lines) {
		line := v.Lines[lineIdx]
		segments := v.wrapSegments(line, contentWidth)
		if sub >= len(segments) {
			lineIdx++
			sub = 0
			continue
		}
		seg := segments[sub]
		display := v.renderSegment(lineIdx, seg.start, seg.end, contentWidth)
		fmt.Fprint(os.Stdout, padRight(truncateANSI(display, width), width))
		fmt.Fprint(os.Stdout, "\r\n")
		row++
		sub++
	}
	for row < contentHeight {
		fmt.Fprint(os.Stdout, strings.Repeat(" ", width))
		fmt.Fprint(os.Stdout, "\r\n")
		row++
	}

	if !v.StatusAtTop {
		fmt.Fprint(os.Stdout, v.renderStatusLine(width))
	}

	v.moveCursorToLine()
	fmt.Fprint(os.Stdout, showCursor)
}

func (v *Viewer) statusLine(width int) string {
	if v.InPrompt {
		return ""
	}
	var parts []string
	if v.Query != "" && len(v.Matches) > 0 {
		parts = append(parts, fmt.Sprintf("match %d/%d", v.MatchIndex+1, len(v.Matches)))
	}
	if v.SelectMode != SelectNone {
		switch v.SelectMode {
		case SelectChar:
			parts = append(parts, "visual")
		case SelectLine:
			parts = append(parts, "visual-line")
		case SelectBlock:
			parts = append(parts, "visual-block")
		}
	}
	if v.Query != "" {
		parts = append(parts, "/"+v.Query)
	}
	if v.Status != "" {
		parts = append(parts, v.Status)
	}
	help := "q quit • / ? search • n/N next • h/j/k/l move • w/b/e word • 0/$/I/A line • g/G top/bot • v/V/ctrl-v select • y yank • L line# • W wrap"
	left := help
	if len(parts) > 0 {
		left = strings.Join(parts, " | ") + " | " + help
	}
	indicator := fmt.Sprintf("%d/%d", v.Cursor+1, len(v.Lines))
	if left == "" {
		return padLeft(indicator, width)
	}
	available := width - visibleWidth(indicator)
	if available < 1 {
		return padLeft(indicator, width)
	}
	left = padRight(left, available)
	return left + indicator
}

func (v *Viewer) renderStatusLine(width int) string {
	// Clear line, then paint full-width status bar background.
	text := v.statusLine(width)
	visible := visibleWidth(text)
	if visible < width {
		text += strings.Repeat(" ", width-visible)
	} else if visible > width {
		text = truncateANSI(text, width)
	}
	return statusBG + statusFG + text + resetStyle
}

func (v *Viewer) moveCursorToLine() {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width, height = 80, 24
	}
	contentHeight := height - 1
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentWidth := v.contentWidthFromHeight()
	row := v.cursorRow(contentHeight, contentWidth)
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
	displayCol := v.CursorCol
	if v.Wrap && contentWidth > 0 {
		displayCol = v.CursorCol % contentWidth
	} else {
		displayCol = v.CursorCol - v.HOffset
	}
	if displayCol < 0 {
		displayCol = 0
	}
	if contentWidth > 0 && displayCol >= contentWidth {
		displayCol = contentWidth - 1
	}
	col := 1 + displayCol
	if v.LineNumbers {
		col = v.lineNumberWidth() + 2 + displayCol
	}
	if col < 1 {
		col = 1
	}
	if col > width {
		col = width
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

func (v *Viewer) matchColForLine(lineIdx int) int {
	if lineIdx < 0 || lineIdx >= len(v.Lines) {
		return 0
	}
	if v.Query == "" {
		return 0
	}
	line := v.Lines[lineIdx]
	lowerLine := strings.ToLower(line)
	lowerQuery := strings.ToLower(v.Query)
	idx := strings.Index(lowerLine, lowerQuery)
	if idx == -1 {
		return 0
	}
	return utf8.RuneCountInString(line[:idx])
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (v *Viewer) contentWidth(totalWidth int) int {
	width := totalWidth
	if v.LineNumbers {
		width -= v.lineNumberWidth() + 1
	}
	if width < 1 {
		width = 1
	}
	return width
}

func (v *Viewer) contentWidthFromHeight() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width = 80
	}
	return v.contentWidth(width)
}

func (v *Viewer) wrapSegments(line string, width int) []segment {
	if width < 1 {
		width = 1
	}
	runes := []rune(line)
	if !v.Wrap {
		return []segment{{start: 0, end: len(runes)}}
	}
	if len(runes) == 0 {
		return []segment{{start: 0, end: 0}}
	}
	var out []segment
	for start := 0; start < len(runes); start += width {
		end := start + width
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, segment{start: start, end: end})
	}
	return out
}

func (v *Viewer) lineSegmentCount(idx int, width int) int {
	if width < 1 {
		width = 1
	}
	if !v.Wrap {
		return 1
	}
	count := v.lineRuneCount(idx)
	if count == 0 {
		return 1
	}
	return (count + width - 1) / width
}

func (v *Viewer) cursorSegmentIndex(width int) int {
	if width < 1 {
		return 0
	}
	if !v.Wrap {
		return 0
	}
	return v.CursorCol / width
}

func (v *Viewer) globalSegIndex(line, seg, width int) int {
	idx := 0
	for i := 0; i < line && i < len(v.Lines); i++ {
		idx += v.lineSegmentCount(i, width)
	}
	return idx + seg
}

func (v *Viewer) fromGlobalSegIndex(idx, width int) (int, int) {
	if idx < 0 {
		return 0, 0
	}
	line := 0
	for line < len(v.Lines) {
		count := v.lineSegmentCount(line, width)
		if idx < count {
			return line, idx
		}
		idx -= count
		line++
	}
	if len(v.Lines) == 0 {
		return 0, 0
	}
	last := len(v.Lines) - 1
	return last, v.lineSegmentCount(last, width) - 1
}

func (v *Viewer) cursorRow(height int, width int) int {
	topGlobal := v.globalSegIndex(v.Top, v.TopSub, width)
	cursorGlobal := v.globalSegIndex(v.Cursor, v.cursorSegmentIndex(width), width)
	return cursorGlobal - topGlobal
}

func (v *Viewer) renderSegment(lineIdx int, segStart int, segEnd int, contentWidth int) string {
	line := v.Lines[lineIdx]
	runes := []rune(line)
	if segStart < 0 {
		segStart = 0
	}
	if segEnd > len(runes) {
		segEnd = len(runes)
	}
	start := segStart
	end := segEnd
	if !v.Wrap {
		start = v.HOffset
		if start < 0 {
			start = 0
		}
		end = start + contentWidth
		if end > len(runes) {
			end = len(runes)
		}
	}
	if start > end {
		start = end
	}
	subRunes := runes[start:end]
	segmentText := string(subRunes)
	ranges := v.selectionRangesForLine(lineIdx)
	var overlaps []segment
	for _, r := range ranges {
		segStart := r.start
		segEnd := r.end
		if segEnd < start || segStart > end {
			continue
		}
		if segStart < start {
			segStart = start
		}
		if segEnd > end {
			segEnd = end
		}
		if segStart >= segEnd {
			continue
		}
		overlaps = append(overlaps, segment{start: segStart - start, end: segEnd - start})
	}
	if len(overlaps) == 0 {
		text := v.applyColors(segmentText, lineIdx)
		if v.LineNumbers {
			prefix := fmt.Sprintf("%*d ", v.lineNumberWidth(), lineIdx+1)
			return prefix + text
		}
		return text
	}
	var out strings.Builder
	pos := 0
	for _, r := range overlaps {
		if r.start > pos {
			out.WriteString(v.applyColors(string(subRunes[pos:r.start]), lineIdx))
		}
		highlight := v.applyColors(string(subRunes[r.start:r.end]), lineIdx)
		out.WriteString(applyReverse(highlight))
		pos = r.end
	}
	if pos < len(subRunes) {
		out.WriteString(v.applyColors(string(subRunes[pos:]), lineIdx))
	}
	if v.LineNumbers {
		prefix := fmt.Sprintf("%*d ", v.lineNumberWidth(), lineIdx+1)
		return prefix + out.String()
	}
	return out.String()
}

func (v *Viewer) applyColors(text string, lineIdx int) string {
	if v.Plain {
		return text
	}
	out := color.ApplyRules(text, v.Rules)
	return color.HighlightQuery(out, v.Query)
}

func (v *Viewer) prompt(reader *bufio.Reader, prefix string) (string, bool) {
	v.Status = ""
	v.InPrompt = true
	defer func() {
		v.InPrompt = false
	}()
	width, _, _ := term.GetSize(int(os.Stdout.Fd()))
	v.renderPrompt(prefix, width)

	var buf []rune
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return string(buf), false
		}
		switch b {
		case '\r', '\n':
			return string(buf), false
		case 0x1b:
			return "", true
		case 0x7f, 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				v.renderPrompt(prefix+string(buf), width)
			}
		default:
			if b < 32 {
				continue
			}
			r, _ := utf8.DecodeRune([]byte{b})
			buf = append(buf, r)
			v.renderPrompt(prefix+string(buf), width)
		}
	}
}

func (v *Viewer) renderPrompt(text string, width int) {
	line := padRight(text, width)
	if v.StatusAtTop {
		fmt.Fprint(os.Stdout, moveHome)
	} else {
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H", v.terminalHeight())
	}
	fmt.Fprint(os.Stdout, statusBG+statusFG+line+resetStyle)
	if v.StatusAtTop {
		fmt.Fprintf(os.Stdout, "\x1b[1;%dH", len(stripANSI(text))+1)
	} else {
		fmt.Fprintf(os.Stdout, "\x1b[%d;%dH", v.terminalHeight(), len(stripANSI(text))+1)
	}
}

func (v *Viewer) terminalHeight() int {
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 24
	}
	return height
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
	v.applyGoalCol()
	v.Status = ""
}

func (v *Viewer) moveCursorCol(delta int) {
	v.CursorCol += delta
	v.clampCursor()
	v.GoalCol = v.CursorCol
	v.Status = ""
}

func (v *Viewer) moveLineStart() {
	v.CursorCol = 0
	v.clampCursor()
	v.GoalCol = v.CursorCol
	v.Status = ""
}

func (v *Viewer) moveLineEnd() {
	maxCol := v.lineRuneCount(v.Cursor)
	if maxCol > 0 {
		maxCol--
	}
	v.CursorCol = maxCol
	v.clampCursor()
	v.GoalCol = v.CursorCol
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
			v.GoalCol = v.CursorCol
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
		if lineIdx < 0 {
			v.Cursor = 0
			v.CursorCol = 0
			v.clampCursor()
			v.Status = ""
			return
		}
		line := []rune(v.Lines[lineIdx])
		if len(line) == 0 {
			lineIdx--
			col = 0
			continue
		}
		if col > len(line)-1 {
			col = len(line) - 1
		}
		if col < 0 {
			col = len(line) - 1
		}
		// move left at least one position
		if col == 0 {
			lineIdx--
			if lineIdx >= 0 {
				prev := []rune(v.Lines[lineIdx])
				col = len(prev) - 1
			}
			continue
		}
		col--
		// skip non-word runes
		for {
			if lineIdx < 0 {
				v.Cursor = 0
				v.CursorCol = 0
				v.clampCursor()
				v.Status = ""
				return
			}
			line = []rune(v.Lines[lineIdx])
			if len(line) == 0 {
				lineIdx--
				col = 0
				continue
			}
			if col < 0 {
				lineIdx--
				if lineIdx >= 0 {
					prev := []rune(v.Lines[lineIdx])
					col = len(prev) - 1
					continue
				}
				v.Cursor = 0
				v.CursorCol = 0
				v.clampCursor()
				v.Status = ""
				return
			}
			if isWordRune(line[col]) {
				break
			}
			col--
		}
		// move to start of word
		for col > 0 && isWordRune(line[col-1]) {
			col--
		}
		v.Cursor = lineIdx
		v.CursorCol = col
		v.clampCursor()
		v.GoalCol = v.CursorCol
		v.Status = ""
		return
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
			v.GoalCol = v.CursorCol
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

func (v *Viewer) toggleWrap() {
	v.Wrap = !v.Wrap
	v.HOffset = 0
	v.TopSub = 0
	v.Status = ""
}

func (v *Viewer) maxHOffset() int {
	width := v.contentWidthFromHeight()
	lineLen := v.lineRuneCount(v.Cursor)
	max := lineLen - width
	if max < 0 {
		return 0
	}
	return max
}

func (v *Viewer) page(delta int) {
	_, height, _ := term.GetSize(int(os.Stdout.Fd()))
	contentHeight := height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	v.Cursor += delta * contentHeight
	v.clampCursor()
	v.applyGoalCol()
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

func (v *Viewer) applyGoalCol() {
	if v.GoalCol < 0 {
		v.GoalCol = 0
	}
	maxCol := v.lineRuneCount(v.Cursor)
	if maxCol > 0 {
		maxCol--
	}
	if v.GoalCol > maxCol {
		v.CursorCol = maxCol
	} else {
		v.CursorCol = v.GoalCol
	}
	if v.CursorCol < 0 {
		v.CursorCol = 0
	}
}

func (v *Viewer) ensureVisible(height int, width int) {
	if width < 1 {
		width = 1
	}
	cursorSeg := v.cursorSegmentIndex(width)
	topGlobal := v.globalSegIndex(v.Top, v.TopSub, width)
	cursorGlobal := v.globalSegIndex(v.Cursor, cursorSeg, width)
	if cursorGlobal < topGlobal {
		topGlobal = cursorGlobal
	}
	if cursorGlobal >= topGlobal+height {
		topGlobal = cursorGlobal - (height - 1)
	}
	v.Top, v.TopSub = v.fromGlobalSegIndex(topGlobal, width)
	if !v.Wrap {
		if v.CursorCol < v.HOffset {
			v.HOffset = v.CursorCol
		}
		if v.CursorCol >= v.HOffset+width {
			v.HOffset = v.CursorCol - width + 1
		}
		maxH := v.maxHOffset()
		if v.HOffset > maxH {
			v.HOffset = maxH
		}
	}
}

func (v *Viewer) cursorTop() {
	v.Cursor = 0
	v.CursorCol = 0
	v.GoalCol = 0
	v.Status = ""
}

func (v *Viewer) cursorBottom() {
	if len(v.Lines) == 0 {
		v.Cursor = 0
		v.CursorCol = 0
		v.GoalCol = 0
		return
	}
	v.Cursor = len(v.Lines) - 1
	v.CursorCol = 0
	v.GoalCol = 0
	v.Status = ""
}

func (v *Viewer) setQuery(query string, dir int) {
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
	v.MatchIndex = v.closestMatchIndex(dir)
	v.Cursor = v.Matches[v.MatchIndex]
	v.CursorCol = v.matchColForLine(v.Cursor)
	v.GoalCol = v.CursorCol
	v.Status = ""
}

func (v *Viewer) closestMatchIndex(dir int) int {
	if len(v.Matches) == 0 {
		return 0
	}
	if dir >= 0 {
		for i, line := range v.Matches {
			if line >= v.Cursor {
				return i
			}
		}
		return 0
	}
	for i := len(v.Matches) - 1; i >= 0; i-- {
		if v.Matches[i] <= v.Cursor {
			return i
		}
	}
	return len(v.Matches) - 1
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
	v.CursorCol = v.matchColForLine(v.Cursor)
	v.GoalCol = v.CursorCol
	v.Status = ""
}

type posRange struct {
	start int
	end   int
}

func (v *Viewer) toggleSelect(mode SelectionMode) {
	if v.SelectMode == mode && v.SelectStart != nil {
		v.clearSelection()
		return
	}
	v.SelectMode = mode
	startCol := v.CursorCol
	if mode == SelectBlock {
		startCol = v.GoalCol
	}
	v.SelectStart = &Position{Line: v.Cursor, Col: startCol}
	switch mode {
	case SelectChar:
		v.Status = "visual"
	case SelectLine:
		v.Status = "visual-line"
	case SelectBlock:
		v.Status = "visual-block"
	}
}

func (v *Viewer) clearSelection() {
	v.SelectMode = SelectNone
	v.SelectStart = nil
	v.Status = "selection cleared"
}

func (v *Viewer) selectionRangesForLine(lineIdx int) []posRange {
	if v.SelectMode == SelectNone || v.SelectStart == nil {
		return nil
	}
	start := *v.SelectStart
	endCol := v.CursorCol
	if v.SelectMode == SelectBlock {
		endCol = v.GoalCol
	}
	end := Position{Line: v.Cursor, Col: endCol}
	minLine, maxLine := start.Line, end.Line
	if minLine > maxLine {
		minLine, maxLine = maxLine, minLine
	}
	if lineIdx < minLine || lineIdx > maxLine {
		return nil
	}
	lineLen := v.lineRuneCount(lineIdx)
	switch v.SelectMode {
	case SelectLine:
		return []posRange{{start: 0, end: lineLen}}
	case SelectBlock:
		minCol, maxCol := start.Col, end.Col
		if minCol > maxCol {
			minCol, maxCol = maxCol, minCol
		}
		if minCol < 0 {
			minCol = 0
		}
		if maxCol >= lineLen {
			maxCol = lineLen - 1
		}
		if lineLen == 0 || minCol > maxCol {
			return nil
		}
		return []posRange{{start: minCol, end: maxCol + 1}}
	case SelectChar:
		s := start
		e := end
		if s.Line > e.Line || (s.Line == e.Line && s.Col > e.Col) {
			s, e = e, s
		}
		if s.Line == e.Line {
			minCol, maxCol := s.Col, e.Col
			if minCol > maxCol {
				minCol, maxCol = maxCol, minCol
			}
			if minCol < 0 {
				minCol = 0
			}
			if maxCol >= lineLen {
				maxCol = lineLen - 1
			}
			if lineLen == 0 || minCol > maxCol {
				return nil
			}
			return []posRange{{start: minCol, end: maxCol + 1}}
		}
		if lineIdx == s.Line {
			startCol := s.Col
			if startCol < 0 {
				startCol = 0
			}
			if startCol >= lineLen {
				return nil
			}
			return []posRange{{start: startCol, end: lineLen}}
		}
		if lineIdx == e.Line {
			endCol := e.Col
			if endCol < 0 {
				endCol = 0
			}
			if endCol >= lineLen {
				endCol = lineLen - 1
			}
			if lineLen == 0 {
				return nil
			}
			return []posRange{{start: 0, end: endCol + 1}}
		}
		return []posRange{{start: 0, end: lineLen}}
	}
	return nil
}

func (v *Viewer) copySelection() {
	if v.SelectMode == SelectNone || v.SelectStart == nil {
		v.Status = "no selection"
		return
	}
	start := *v.SelectStart
	end := Position{Line: v.Cursor, Col: v.CursorCol}
	minLine, maxLine := start.Line, end.Line
	if minLine > maxLine {
		minLine, maxLine = maxLine, minLine
	}
	if minLine < 0 {
		minLine = 0
	}
	if maxLine >= len(v.Lines) {
		maxLine = len(v.Lines) - 1
	}
	var out []string
	switch v.SelectMode {
	case SelectLine:
		out = append(out, v.Lines[minLine:maxLine+1]...)
	case SelectBlock:
		minCol, maxCol := start.Col, end.Col
		if minCol > maxCol {
			minCol, maxCol = maxCol, minCol
		}
		for i := minLine; i <= maxLine; i++ {
			runes := []rune(v.Lines[i])
			if len(runes) == 0 || minCol >= len(runes) {
				out = append(out, "")
				continue
			}
			endCol := maxCol
			if endCol >= len(runes) {
				endCol = len(runes) - 1
			}
			out = append(out, string(runes[minCol:endCol+1]))
		}
	case SelectChar:
		for i := minLine; i <= maxLine; i++ {
			ranges := v.selectionRangesForLine(i)
			if len(ranges) == 0 {
				out = append(out, "")
				continue
			}
			runes := []rune(v.Lines[i])
			var lineOut strings.Builder
			for _, r := range ranges {
				if r.start < 0 {
					r.start = 0
				}
				if r.end > len(runes) {
					r.end = len(runes)
				}
				if r.start >= r.end {
					continue
				}
				lineOut.WriteString(string(runes[r.start:r.end]))
			}
			out = append(out, lineOut.String())
		}
	}
	text := strings.Join(out, "\n")
	if err := clipboard.WriteAll(text); err != nil {
		v.Status = "clipboard failed"
		return
	}
	v.Status = "copied"
}

func (v *Viewer) appendLines(lines []string) {
	if len(lines) == 0 {
		return
	}
	atEnd := v.Cursor >= len(v.Lines)-1
	v.Lines = append(v.Lines, lines...)
	if v.Follow && atEnd {
		v.Cursor = len(v.Lines) - 1
		v.CursorCol = 0
		v.GoalCol = 0
	}
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

func visibleWidth(s string) int {
	return utf8.RuneCountInString(stripANSI(s))
}

func padLeft(s string, width int) string {
	if width <= 0 {
		return s
	}
	visible := visibleWidth(s)
	if visible >= width {
		return s
	}
	return strings.Repeat(" ", width-visible) + s
}

func applyReverse(s string) string {
	if s == "" {
		return s
	}
	out := strings.ReplaceAll(s, resetStyle, resetStyle+reverseOn)
	return reverseOn + out + reverseOff
}

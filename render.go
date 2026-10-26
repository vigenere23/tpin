package main

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	maxNoteWidth = 60
	reset        = "\x1b[0m"
	bold         = "\x1b[1m"
)

var (
	colorCodes = map[string]string{"red": "31", "green": "32", "yellow": "33", "blue": "34", "magenta": "35", "cyan": "36"}
	headingRe  = regexp.MustCompile(`^#{1,6} +`)
	listRe     = regexp.MustCompile(`^\s*([-*+•◦▪]|\d+[.)]) +`)
	bulletRe   = regexp.MustCompile(`^(\s*)[-*+] `)
	bullets    = []string{"•", "◦", "▪"}
)

type style byte

const (
	strong style = 1 << iota
	heading
	strike
)

type cell struct {
	r     rune
	w     int
	style style
}

type note struct {
	lines []string
	width int
}

func renderBoard(pins []pinView, width int, color bool) string {
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	var row []note
	used := 0
	for _, p := range pins {
		n := renderNote(p, min(width, maxNoteWidth), color)
		if len(row) > 0 && used+1+n.width > width {
			writeRow(&b, row)
			row, used = nil, 0
		}
		if len(row) > 0 {
			used++
		}
		row, used = append(row, n), used+n.width
	}
	writeRow(&b, row)
	return b.String()
}

func writeRow(b *strings.Builder, row []note) {
	height := 0
	for _, n := range row {
		height = max(height, len(n.lines))
	}
	for i := range height {
		parts := make([]string, len(row))
		for j, n := range row {
			if i < len(n.lines) {
				parts[j] = n.lines[i]
			} else {
				parts[j] = strings.Repeat(" ", n.width)
			}
		}
		b.WriteString(strings.TrimRight(strings.Join(parts, " "), " ") + "\n")
	}
}

func renderNote(p pinView, outer int, color bool) note {
	inner := max(outer-4, 1)
	title := ""
	if p.name != defaultPin {
		title = p.name
		if r := []rune(title); len(r) > inner-2 {
			title = string(r[:max(inner-3, 0)]) + "…"
		}
	}
	code := colorCodes[p.eff.Color]
	// Headings take the note's color; uncolored notes underline them so they still differ from bold.
	headingSGR := "\x1b[1;4m"
	if code != "" {
		headingSGR = "\x1b[1;" + code + "m"
	}
	var body [][]cell
	for _, line := range strings.Split(strings.TrimRight(p.text, "\n \t"), "\n") {
		body = append(body, wrap(parseLine(line, color), inner)...)
	}
	w := 0
	if title != "" {
		w = ansi.StringWidth(title) + 2
	}
	rendered := make([]string, len(body))
	for i, cs := range body {
		rendered[i] = styled(cs, color, headingSGR)
		w = max(w, ansi.StringWidth(rendered[i]))
	}
	w = max(min(w, inner), 1)

	paint := func(s string) string { return s }
	if color && code != "" {
		paint = func(s string) string { return "\x1b[" + code + "m" + s + reset }
	}
	top := strings.Repeat("─", w+2)
	if title != "" {
		top = "─ " + title + " " + strings.Repeat("─", max(w-ansi.StringWidth(title)-1, 0))
	}
	n := note{width: w + 4, lines: []string{paint("╭" + top + "╮")}}
	for _, r := range rendered {
		pad := strings.Repeat(" ", max(w-ansi.StringWidth(r), 0))
		n.lines = append(n.lines, paint("│")+" "+r+pad+" "+paint("│"))
	}
	n.lines = append(n.lines, paint("╰"+strings.Repeat("─", w+2)+"╯"))
	return n
}

// parseLine renders bullets and strips heading hashes and emphasis markers,
// recording the style per rune. Without color, ~~ stays so strikes aren't lost.
func parseLine(line string, color bool) []cell {
	line = strings.ReplaceAll(strings.TrimRight(line, " \t"), "\t", "    ")
	var base style
	if m := headingRe.FindString(line); m != "" {
		line, base = line[len(m):], heading
	}
	if m := bulletRe.FindStringSubmatch(line); m != nil {
		line = m[1] + bullets[len(m[1])/2%len(bullets)] + line[len(m[0])-1:]
	}
	toggles := map[int]style{}
	for marker, st := range map[string]style{"**": strong, "~~": strike} {
		var at []int
		for i := 0; ; i += 2 {
			j := strings.Index(line[i:], marker)
			if j < 0 {
				break
			}
			i += j
			at = append(at, i)
		}
		// An unmatched trailing marker stays literal.
		for _, i := range at[:len(at)/2*2] {
			toggles[i] = st
		}
	}
	var cs []cell
	cur := base
	for i := 0; i < len(line); {
		if st, ok := toggles[i]; ok {
			cur ^= st
			if st != strike || color {
				i += 2
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(line[i:])
		cs = append(cs, cell{r: r, w: ansi.StringWidth(string(r)), style: cur})
		i += n
	}
	return cs
}

func width(cs []cell) int {
	w := 0
	for _, c := range cs {
		w += c.w
	}
	return w
}

func trimSpace(cs []cell, left bool) []cell {
	for len(cs) > 0 {
		i := len(cs) - 1
		if left {
			i = 0
		}
		if cs[i].r != ' ' {
			break
		}
		if left {
			cs = cs[1:]
		} else {
			cs = cs[:i]
		}
	}
	return cs
}

// wrap breaks at spaces, hard-breaking overlong words; continuation lines hang
// under a list item's text, or under a plain line's own indentation.
func wrap(cs []cell, w int) [][]cell {
	lead := len(cs) - len(trimSpace(cs, true))
	hang := lead
	if m := listRe.FindString(string(runes(cs))); m != "" {
		hang = len([]rune(m))
	}
	if hang > w/2 {
		hang = 0
	}
	var out [][]cell
	prefix := []cell(nil)
	for {
		avail := w - width(prefix)
		if width(cs) <= avail {
			return append(out, append(prefix, cs...))
		}
		cut, acc, hard := -1, 0, 0
		for i, c := range cs {
			if c.r == ' ' && i > lead {
				cut = i
			}
			if acc+c.w > avail {
				break
			}
			acc += c.w
			hard = i + 1
		}
		if cut < 0 {
			cut = max(hard, 1)
		}
		out = append(out, append(prefix, trimSpace(cs[:cut], false)...))
		cs, lead = trimSpace(cs[cut:], true), 0
		if len(cs) == 0 {
			return out
		}
		prefix = []cell(nil)
		for range hang {
			prefix = append(prefix, cell{r: ' ', w: 1})
		}
	}
}

func runes(cs []cell) []rune {
	rs := make([]rune, len(cs))
	for i, c := range cs {
		rs[i] = c.r
	}
	return rs
}

// styled resets at the end of every line so each line is self-contained.
func styled(cs []cell, color bool, headingSGR string) string {
	var b strings.Builder
	var cur style
	for _, c := range cs {
		if color && c.style != cur {
			if cur != 0 {
				b.WriteString(reset)
			}
			cur = c.style
			switch {
			case cur&heading != 0:
				b.WriteString(headingSGR)
			case cur&strong != 0:
				b.WriteString(bold)
			}
			if cur&strike != 0 {
				b.WriteString("\x1b[2;9m")
			}
		}
		b.WriteRune(c.r)
	}
	if cur != 0 {
		b.WriteString(reset)
	}
	return b.String()
}

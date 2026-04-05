package panel

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const boxWidth = 52 // visible characters inside the ║ borders

var spinFrames = []rune("⠋⠙⠸⠼⠴⠦⠧⠇⠏⠛")

type roleState int

const (
	stateWaiting roleState = iota
	stateRunning
	stateDone
	stateFailed
)

type roleTrack struct {
	name    string
	model   string
	state   roleState
	steps   int
	dur     time.Duration
	started time.Time
}

// panelDisplay renders a live ASCII progress box to an io.Writer.
// It is goroutine-safe: role goroutines call markRunning/markDone;
// a ticker goroutine calls render periodically.
type panelDisplay struct {
	mu    sync.Mutex
	roles []roleTrack
	w     io.Writer
	frame int
	lines int // lines written in the last render (used for cursor-up)
	start time.Time
}

func newPanelDisplay(w io.Writer, roles []Role, defaultModel string) *panelDisplay {
	tracks := make([]roleTrack, len(roles))
	for i, r := range roles {
		m := r.Model
		if m == "" {
			m = defaultModel
		}
		tracks[i] = roleTrack{name: r.Name, model: m}
	}
	return &panelDisplay{w: w, roles: tracks, start: time.Now()}
}

func (d *panelDisplay) markRunning(idx int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if idx >= 0 && idx < len(d.roles) {
		d.roles[idx].state = stateRunning
		d.roles[idx].started = time.Now()
	}
}

func (d *panelDisplay) markDone(idx int, steps int, dur time.Duration, failed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if idx >= 0 && idx < len(d.roles) {
		if failed {
			d.roles[idx].state = stateFailed
		} else {
			d.roles[idx].state = stateDone
		}
		d.roles[idx].steps = steps
		d.roles[idx].dur = dur
	}
}

// render redraws the full status box in-place using ANSI cursor-up.
// Call with final=true for the last render (shows total duration).
func (d *panelDisplay) render(final bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.frame = (d.frame + 1) % len(spinFrames)

	// Move cursor up to overwrite the previous render.
	if d.lines > 0 {
		fmt.Fprintf(d.w, "\033[%dA", d.lines)
	}

	var sb strings.Builder

	// Top border + title line.
	sb.WriteString("╔" + strings.Repeat("═", boxWidth) + "╗\n")
	hdr := fmt.Sprintf(" Panel · %d role", len(d.roles))
	if len(d.roles) != 1 {
		hdr += "s"
	}
	if final {
		hdr += fmt.Sprintf(" · done in %s", time.Since(d.start).Round(time.Millisecond))
	} else {
		hdr += fmt.Sprintf(" · %s", time.Since(d.start).Round(time.Second))
	}
	writeBoxLine(&sb, hdr)
	sb.WriteString("╠" + strings.Repeat("═", boxWidth) + "╣\n")

	// One line per role.
	for _, r := range d.roles {
		var icon, status string
		switch r.state {
		case stateWaiting:
			icon = "○"
			status = "waiting..."
		case stateRunning:
			icon = string(spinFrames[d.frame])
			status = time.Since(r.started).Round(time.Second).String()
		case stateDone:
			icon = "✓"
			status = fmt.Sprintf("%d steps  %s", r.steps, r.dur.Round(time.Millisecond))
		case stateFailed:
			icon = "✗"
			status = "failed  " + r.dur.Round(time.Millisecond).String()
		}
		content := fmt.Sprintf("  %s  %-13s  %-18s  %s",
			icon,
			runesTrunc(r.name, 13),
			runesTrunc(r.model, 18),
			status,
		)
		writeBoxLine(&sb, content)
	}

	// Bottom border.
	sb.WriteString("╚" + strings.Repeat("═", boxWidth) + "╝\n")

	out := sb.String()
	fmt.Fprint(d.w, out)
	d.lines = strings.Count(out, "\n")
}

// writeBoxLine writes one line inside the box, padded or truncated to boxWidth.
func writeBoxLine(sb *strings.Builder, content string) {
	runes := []rune(content)
	if len(runes) > boxWidth {
		runes = runes[:boxWidth]
	}
	sb.WriteString("║")
	sb.WriteString(string(runes))
	for i := len(runes); i < boxWidth; i++ {
		sb.WriteByte(' ')
	}
	sb.WriteString("║\n")
}

// runesTrunc returns s padded with spaces to exactly n runes,
// or truncated with "…" if longer.
func runesTrunc(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		if n > 1 {
			return string(runes[:n-1]) + "…"
		}
		return string(runes[:n])
	}
	return s + strings.Repeat(" ", n-len(runes))
}

package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// highlightDuration is how long a changed cell stays highlighted.
	highlightDuration = 2 * time.Second
	// tickInterval is the refresh rate for expiring highlights.
	tickInterval = 250 * time.Millisecond
	// cellWidth is the width of one shard column.
	cellWidth = 14
	// uidWidth is the width of the UID column.
	uidWidth = 10
	// uidPrefix is the number of UID characters shown, enough to compare UIDs by eye.
	uidPrefix = 8
	// countWidth is the width of one shard column in the resource summary.
	countWidth = 9
)

// shardColors are assigned to shards in flag order and wrap around.
var shardColors = []string{"245", "39", "42", "214", "205"}

var (
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleMultiple  = lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true)
	styleHighlight = lipgloss.NewStyle().Reverse(true)
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// model is the bubbletea model of etcdview.
type model struct {
	state     *state
	cluster   string
	endpoints []endpoint
	msgs      <-chan tea.Msg
	status    map[string]statusMsg
	width     int
	height    int
	offset    int
	now       func() time.Time
}

func newModel(cluster string, endpoints []endpoint, msgs <-chan tea.Msg) model {
	shards := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		shards = append(shards, ep.shard)
	}
	return model{
		state:     newState(shards),
		cluster:   cluster,
		endpoints: endpoints,
		msgs:      msgs,
		status:    map[string]statusMsg{},
		now:       time.Now,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitForMsg(m.msgs), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case listMsg:
		m.state.applyList(msg.shard, msg.items, m.now())
		return m, waitForMsg(m.msgs)
	case eventsMsg:
		m.state.applyEvents(msg.shard, msg.items, m.now())
		return m, waitForMsg(m.msgs)
	case statusMsg:
		m.status[msg.shard] = msg
		return m, waitForMsg(m.msgs)
	case tickMsg:
		m.state.prune(m.now().Add(-highlightDuration))
		return m, tick()
	default:
		return m, nil
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (model, tea.Cmd) {
	page := max(m.listHeight()-1, 1)
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "pgdown", "space":
		m.offset += page
	case "pgup":
		m.offset -= page
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = len(m.state.rows)
	default:
		return m, nil
	}
	m.offset = m.clampOffset(m.offset)
	return m, nil
}

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(m.summary())
	b.WriteString(m.keys())
	b.WriteString(m.footer())

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m model) header() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("etcdview") + "  logical cluster " + styleTitle.Render(m.cluster) + "\n")
	for _, ep := range m.endpoints {
		st := m.status[ep.shard]
		line := "connecting"
		switch {
		case st.err != nil:
			line = styleError.Render(st.err.Error())
		case st.status != "":
			line = st.status
		}
		fmt.Fprintf(&b, "%s %s %s\n",
			m.shardStyle(ep.shard).Render(pad(ep.shard, cellWidth)),
			styleDim.Render(ep.url),
			line,
		)
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) summary() string {
	var b strings.Builder
	counts := m.state.countByResource()
	longest := len("RESOURCE")
	for _, rc := range counts {
		longest = max(longest, len(rc.resource))
	}
	resourceWidth := min(longest+2, m.keyWidth(countWidth))

	b.WriteString(styleDim.Render(pad("RESOURCE", resourceWidth)))
	for _, shard := range m.state.shards {
		b.WriteString(m.shardStyle(shard).Render(padLeft(shard, countWidth)))
	}
	b.WriteString("\n")

	totals := map[string]int{}
	for _, rc := range counts {
		b.WriteString(pad(rc.resource, resourceWidth))
		for _, shard := range m.state.shards {
			totals[shard] += rc.counts[shard]
			b.WriteString(countCell(rc.counts[shard]))
		}
		b.WriteString("\n")
	}

	b.WriteString(styleTitle.Render(pad("total", resourceWidth)))
	for _, shard := range m.state.shards {
		b.WriteString(styleTitle.Render(countCell(totals[shard])))
	}
	b.WriteString("\n\n")
	return b.String()
}

func countCell(n int) string {
	if n == 0 {
		return styleDim.Render(padLeft("-", countWidth))
	}
	return padLeft(fmt.Sprint(n), countWidth)
}

func (m model) keys() string {
	var b strings.Builder
	ids := m.state.sortedIDs()
	longest := len("KEY")
	for _, id := range ids {
		longest = max(longest, len(keyLabel(m.state.rows[id].parts)))
	}
	keyWidth := min(longest+2, m.keyWidth(cellWidth)-uidWidth)

	b.WriteString(styleDim.Render(pad("KEY", keyWidth) + pad("UID", uidWidth)))
	for _, shard := range m.state.shards {
		b.WriteString(m.shardStyle(shard).Render(pad(shard, cellWidth)))
	}
	b.WriteString("\n")

	offset := m.clampOffset(m.offset)
	end := min(offset+m.listHeight(), len(ids))
	now := m.now()
	for _, id := range ids[offset:end] {
		r := m.state.rows[id]
		b.WriteString(m.rowStyle(r).Render(pad(keyLabel(r.parts), keyWidth)))
		b.WriteString(uidCell(r, m.state.shards))
		for _, shard := range m.state.shards {
			b.WriteString(m.cell(shard, r, now))
		}
		b.WriteString("\n")
	}
	for range m.listHeight() - (end - offset) {
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) cell(shard string, r *row, now time.Time) string {
	e, ok := r.shards[shard]
	if !ok {
		return styleDim.Render(pad("·", cellWidth))
	}

	text := fmt.Sprintf("rev %d", e.rev)
	if e.lease != 0 {
		text += " L"
	}
	style := m.shardStyle(shard)
	if e.deleted {
		text = "deleted"
		style = styleError
	}
	if now.Sub(e.changed) < highlightDuration {
		style = style.Inherit(styleHighlight)
	}
	return style.Render(pad(text, cellWidth-1)) + " "
}

// uidCell shows the UID prefix, or a mismatch if shards disagree.
func uidCell(r *row, shards []string) string {
	uid := ""
	for _, shard := range shards {
		e, ok := r.shards[shard]
		if !ok || e.deleted || e.uid == "" {
			continue
		}
		if uid != "" && uid != e.uid {
			return styleError.Render(pad("mismatch", uidWidth))
		}
		uid = e.uid
	}
	if uid == "" {
		return styleDim.Render(pad("?", uidWidth))
	}
	return styleDim.Render(pad(uid[:min(len(uid), uidPrefix)], uidWidth))
}

func (m model) footer() string {
	return styleDim.Render(fmt.Sprintf("%d keys  j/k scroll  space/pgup page  g/G top/bottom  q quit", len(m.state.rows)))
}

// rowStyle colors a key by the shard holding it, a key on several shards is marked separately.
func (m model) rowStyle(r *row) lipgloss.Style {
	present := r.present(m.state.shards)
	switch len(present) {
	case 0:
		return styleDim
	case 1:
		return m.shardStyle(present[0])
	default:
		return styleMultiple
	}
}

func (m model) shardStyle(shard string) lipgloss.Style {
	for i, s := range m.state.shards {
		if s == shard {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(shardColors[i%len(shardColors)]))
		}
	}
	return lipgloss.NewStyle()
}

// keyWidth returns the width left for the first column next to one column per shard.
func (m model) keyWidth(columnWidth int) int {
	return max(m.width-len(m.state.shards)*columnWidth, 20)
}

// listHeight returns the number of key rows fitting below header and summary.
func (m model) listHeight() int {
	used := strings.Count(m.header(), "\n") + strings.Count(m.summary(), "\n") + 2
	return max(m.height-used, 1)
}

func (m model) clampOffset(offset int) int {
	return max(min(offset, len(m.state.rows)-m.listHeight()), 0)
}

// keyLabel renders a key as group/resource [segment] [namespace/]name.
func keyLabel(p KeyParts) string {
	label := p.Group + "/" + p.Resource
	if p.Segment != "" && p.Segment != "customresources" {
		label += " [" + p.Segment + "]"
	}
	return label + " " + p.Rest
}

// pad truncates or pads s to width.
func pad(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width-1]) + " "
	}
	return s + strings.Repeat(" ", width-len(r))
}

func padLeft(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return strings.Repeat(" ", width-len(r)) + s
}

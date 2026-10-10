package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"heron/internal/render"
)

// plantumlTimeout bounds one conversion batch so a wedged plantuml process
// cannot leave a block pending forever (NFR-7).
const plantumlTimeout = 20 * time.Second

// plantumlJob is one pending conversion: the stable block id plus the raw
// source that stays visible until the result arrives.
type plantumlJob struct {
	id   int
	code string
}

// plantumlResult is the outcome for a single block. ok=false means the
// conversion failed — the raw source then remains (FR-6 fallback).
type plantumlResult struct {
	id    int
	ascii string
	ok    bool
}

// plantumlDoneMsg carries every result of one conversion batch, keyed by the
// stable block id.
type plantumlDoneMsg struct {
	results []plantumlResult
}

// plantUMLJobs lists the blocks that still need conversion.
func (m Model) plantUMLJobs() []plantumlJob {
	var jobs []plantumlJob
	for _, it := range m.chat.items {
		cb, ok := it.(CodeBlock)
		if !ok || cb.lang != "plantuml" || cb.converted || cb.id < 1 {
			continue
		}
		jobs = append(jobs, plantumlJob{id: cb.id, code: cb.code})
	}
	return jobs
}

// pendingPlantUML starts the ASCII conversion for all pending blocks. It runs
// as a tea.Cmd (goroutine), so the event loop is never blocked (NFR-2); the
// block keeps showing the raw source until plantumlDoneMsg arrives.
func (m Model) pendingPlantUML() tea.Cmd {
	jobs := m.plantUMLJobs()
	if len(jobs) == 0 {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), plantumlTimeout)
		defer cancel()
		results := make([]plantumlResult, 0, len(jobs))
		for _, j := range jobs {
			ascii, err := render.PlantUMLToASCII(ctx, j.code)
			results = append(results, plantumlResult{
				id:    j.id,
				ascii: ascii,
				ok:    err == nil,
			})
		}
		return plantumlDoneMsg{results: results}
	}
}

// applyPlantUML installs the conversion results in the chat (FR-6): the ASCII
// replaces the source on success, otherwise the raw source stays. Either way
// the block is marked converted so it is never queued again.
func (l BlockList) applyPlantUML(results []plantumlResult) (BlockList, int) {
	applied := 0
	for _, r := range results {
		for i, it := range l.items {
			cb, ok := it.(CodeBlock)
			if !ok || cb.id != r.id {
				continue
			}
			nb := cb
			nb.converted = true
			if r.ok && strings.TrimSpace(r.ascii) != "" {
				nb.code = strings.TrimSuffix(r.ascii, "\n")
			}
			l.items[i] = nb
			applied++
			break
		}
	}
	return l, applied
}

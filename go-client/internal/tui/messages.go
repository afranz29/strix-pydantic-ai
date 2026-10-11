package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/strix/go-client/internal/api"
)

type ScanStartedMsg struct {
	ScanID string
}

type EventReceivedMsg struct {
	Event  interface{}
	Stream *api.EventStream
}

type ErrorMsg struct {
	Err error
}

type TickMsg time.Time

func (m Model) startScan() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.timeout)*time.Second)
		defer cancel()

		req := &api.ScanRequest{
			Target:      m.target,
			ScanMode:    m.scanMode,
			Model:       m.model,
			MockTools:   m.mockTools,
			Instruction: m.instruction,
			Skills:      m.skills,
			Timeout:     m.timeout,
			Verbose:     m.verbose,
		}

		resp, err := m.apiClient.StartScan(ctx, req)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return ScanStartedMsg{ScanID: resp.ScanID}
	}
}

func (m Model) streamEvents() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		stream, err := m.apiClient.StreamEvents(ctx, m.scanID)
		if err != nil {
			return ErrorMsg{Err: err}
		}

		return readNextEvent(stream)()
	}
}

func ReadNextEvent(stream *api.EventStream) tea.Cmd {
	return func() tea.Msg {
		if stream == nil {
			return nil
		}

		event, ok := stream.Next()
		if !ok {
			return nil
		}

		return EventReceivedMsg{Event: event, Stream: stream}
	}
}

func readNextEvent(stream *api.EventStream) tea.Cmd {
	return ReadNextEvent(stream)
}

func tickEvery() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

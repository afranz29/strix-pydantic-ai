package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) StartScan(ctx context.Context, req *ScanRequest) (*ScanResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/scans", c.baseURL), bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var scanResp ScanResponse
	if err := json.NewDecoder(resp.Body).Decode(&scanResp); err != nil {
		return nil, err
	}

	return &scanResp, nil
}

type EventStream struct {
	events chan interface{}
	done   chan struct{}
	err    error
}

func NewEventStream(resp *http.Response) *EventStream {
	es := &EventStream{
		events: make(chan interface{}, 100),
		done:   make(chan struct{}),
	}

	go func() {
		defer close(es.events)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			event, err := parseEvent(line)
			if err != nil {
				es.err = err
				continue
			}

			if event == nil {
				continue
			}

			select {
			case es.events <- event:
			case <-es.done:
				return
			}
		}

		if err := scanner.Err(); err != nil {
			es.err = err
		}
	}()

	return es
}

func (es *EventStream) Next() (interface{}, bool) {
	event, ok := <-es.events
	return event, ok
}

func (es *EventStream) Err() error {
	return es.err
}

func (es *EventStream) Close() {
	close(es.done)
}

func parseEvent(data []byte) (interface{}, error) {
	var baseEvent Event
	if err := json.Unmarshal(data, &baseEvent); err != nil {
		return nil, err
	}

	switch baseEvent.Type {
	case "scan_started":
		var event ScanStartedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "scan_configured":
		var event ScanConfiguredEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "agent_started":
		var event AgentStartedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "agent_message":
		var event AgentMessageEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "agent_completed":
		var event AgentCompletedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "vulnerability_found":
		var event VulnerabilityFoundEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "agent_thinking":
		var event AgentThinkingEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "agent_token_usage":
		var event AgentTokenUsageEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "tool_executed":
		var event ToolExecutedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "log_message":
		var event LogMessageEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "scan_completed":
		var event ScanCompletedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	case "scan_failed":
		var event ScanFailedEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		return event, nil

	default:
		return baseEvent, nil
	}
}

func (c *Client) StreamEvents(ctx context.Context, scanID string) (*EventStream, error) {
	url := fmt.Sprintf("%s/scans/%s/events", c.baseURL, scanID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return NewEventStream(resp), nil
}

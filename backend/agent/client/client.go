package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	managerURL string
	token      string
	httpClient *http.Client
}

type AgentInfo struct {
	AgentType string            `json:"agent_type,omitempty"`
	Hostname  string            `json:"hostname,omitempty"`
	Os        string            `json:"os,omitempty"`
	Arch      string            `json:"arch,omitempty"`
	Ip        string            `json:"ip,omitempty"`
	Version   string            `json:"version,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type ConnectRequest struct {
	Info *AgentInfo `json:"info,omitempty"`
}

func New(managerURL, token string) *Client {
	managerURL = strings.TrimRight(managerURL, "/")
	return &Client{
		managerURL: managerURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Connect(info *AgentInfo) error {
	req := ConnectRequest{Info: info}
	return c.doPost("/v1/agents:connect", req)
}

func (c *Client) Heartbeat() error {
	return c.doPost("/v1/agents:heartbeat", map[string]any{})
}

func (c *Client) doPost(path string, body any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.managerURL+path, &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

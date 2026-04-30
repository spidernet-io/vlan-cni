// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package spclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// SpiderpoolAgentSocket is the well-known Unix socket path for spiderpool-agent.
	// This is the same socket used by spiderpool IPAM (cmd/spiderpool).
	SpiderpoolAgentSocket = "/var/run/spidernet/spiderpool.sock"

	defaultTimeout = 10 * time.Second
)

// Client communicates with spiderpool-agent via Unix socket
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new spiderpool-agent Unix socket client.
// socketPath is typically SpiderpoolAgentSocket.
func NewClient(socketPath string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
				},
			},
		},
		// Use "http://localhost" as base; the actual routing is via Unix socket
		baseURL: "http://localhost",
	}
}

// GetWorkloadEndpoint queries spiderpool-agent for the Pod's network assignment.
// podName and podNamespace are required. nic is optional (CNI_IFNAME).
func (c *Client) GetWorkloadEndpoint(podName, podNamespace, nic string) (*IPAssignment, error) {
	if podName == "" || podNamespace == "" {
		return nil, fmt.Errorf("podName and podNamespace are required")
	}

	// Build request URL with query parameters
	reqURL := fmt.Sprintf("%s/v1/workloadendpoint?podName=%s&podNamespace=%s",
		c.baseURL, podName, podNamespace)
	if nic != "" {
		reqURL += "&nic=" + nic
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to spiderpool-agent: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spiderpool-agent returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var wlResp WorkloadEndpointResponse
	if err := json.Unmarshal(bodyBytes, &wlResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if len(wlResp.IPAssignments) == 0 {
		return nil, fmt.Errorf("spiderpool-agent returned no IP assignments")
	}

	// Find matching NIC entry
	if nic != "" {
		assignment, ok := wlResp.IPAssignments[nic]
		if !ok {
			return nil, fmt.Errorf("no assignment for nic %q", nic)
		}
		return &assignment, nil
	}

	// No nic specified: return the first entry
	for _, assignment := range wlResp.IPAssignments {
		a := assignment
		return &a, nil
	}

	return nil, fmt.Errorf("spiderpool-agent returned no IP assignments")
}

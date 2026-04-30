// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package spclient

// IPAssignment represents a single NIC's network assignment from spiderpool-agent
type IPAssignment struct {
	IPs    []string `json:"ips"`              // e.g. ["192.168.1.100/24"]
	VlanID int      `json:"vlanId"`
	MAC    string   `json:"mac,omitempty"`
}

// WorkloadEndpointResponse is the response from GetWorkloadEndpoint
type WorkloadEndpointResponse struct {
	IPAssignments map[string]IPAssignment `json:"ipAssignments"` // keyed by NIC name
}

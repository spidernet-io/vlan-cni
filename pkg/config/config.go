// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
)

// NetConf represents the CNI network configuration
type NetConf struct {
	types.NetConf
	Master     string `json:"master"`                    // Master interface name (required)
	VlanID     *int   `json:"vlanId,omitempty"`          // VLAN ID (0-4094). nil = service mode, non-nil = standard mode
	MTU        int    `json:"mtu,omitempty"`
	LinkContNs bool   `json:"linkInContainer,omitempty"`
}

// LoadConf loads and validates the CNI configuration
func LoadConf(args *skel.CmdArgs) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(args.StdinData, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}

	if n.Master == "" {
		return nil, "", fmt.Errorf("\"master\" field is required")
	}

	if n.VlanID != nil {
		// Standard mode: validate vlanId range
		if *n.VlanID < 0 || *n.VlanID > 4094 {
			return nil, "", fmt.Errorf("invalid vlanId %d (must be 0-4094)", *n.VlanID)
		}
	}
	// Service mode (VlanID == nil): no additional config validation needed,
	// will connect to spiderpool-agent Unix socket at runtime

	return n, n.CNIVersion, nil
}

// IsServiceMode returns true if service mode is enabled (vlanId not set)
func (n *NetConf) IsServiceMode() bool {
	return n.VlanID == nil
}

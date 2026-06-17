// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
)

const (
	VlanModeManual = "manual"
	VlanModeAuto   = "auto"
)

// NetConf represents the CNI network configuration
type NetConf struct {
	types.NetConf
	Master     string `json:"master"`             // Master interface name (required)
	VlanMode   string `json:"vlanMode,omitempty"` // VLAN mode: manual or auto
	VlanID     *int   `json:"vlanId,omitempty"`   // VLAN ID (0-4094). Defaults to 0 in manual mode
	MTU        int    `json:"mtu,omitempty"`
	LinkContNs bool   `json:"linkInContainer,omitempty"`
}

// LoadConf loads and validates the CNI configuration
func LoadConf(args *skel.CmdArgs) (*NetConf, string, error) {
	n := &NetConf{}
	if err := json.Unmarshal(args.StdinData, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %w", err)
	}

	if n.Master == "" {
		return nil, "", fmt.Errorf("\"master\" field is required")
	}

	switch n.VlanMode {
	case "":
		// Backward compatibility: existing configs selected mode by vlanId presence.
		if n.VlanID == nil {
			n.VlanMode = VlanModeAuto
		} else {
			n.VlanMode = VlanModeManual
		}
	case VlanModeManual, VlanModeAuto:
	default:
		return nil, "", fmt.Errorf("invalid vlanMode %q (must be %q or %q)", n.VlanMode, VlanModeManual, VlanModeAuto)
	}

	if n.VlanMode == VlanModeManual {
		if n.VlanID == nil {
			vlanID := 0
			n.VlanID = &vlanID
		}
		if *n.VlanID < 0 || *n.VlanID > 4094 {
			return nil, "", fmt.Errorf("invalid vlanId %d (must be 0-4094)", *n.VlanID)
		}
	}
	// Auto mode gets VLAN dynamically from IPAM/spiderpool-agent at runtime.

	return n, n.CNIVersion, nil
}

// IsServiceMode returns true if VLAN auto mode is enabled.
func (n *NetConf) IsServiceMode() bool {
	if n.VlanMode != "" {
		return n.VlanMode == VlanModeAuto
	}
	return n.VlanID == nil
}

// MarshalJSON implements custom JSON marshaling to handle embedded types.NetConf
func (n *NetConf) MarshalJSON() ([]byte, error) {
	// First marshal the embedded NetConf (which has custom MarshalJSON)
	netConfBytes, err := json.Marshal(&n.NetConf)
	if err != nil {
		return nil, err
	}

	// Unmarshal to map to combine with NetConf fields
	var combined map[string]interface{}
	if err := json.Unmarshal(netConfBytes, &combined); err != nil {
		return nil, err
	}

	// Add NetConf-specific fields
	if n.Master != "" {
		combined["master"] = n.Master
	}
	if n.VlanMode != "" {
		combined["vlanMode"] = n.VlanMode
	}
	if n.VlanID != nil {
		combined["vlanId"] = *n.VlanID
	}
	if n.MTU != 0 {
		combined["mtu"] = n.MTU
	}
	if n.LinkContNs {
		combined["linkInContainer"] = n.LinkContNs
	}

	return json.Marshal(combined)
}

// UnmarshalJSON implements custom JSON unmarshaling to handle embedded types.NetConf
func (n *NetConf) UnmarshalJSON(data []byte) error {
	// First unmarshal to a map to extract NetConf fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Unmarshal the embedded NetConf
	netConfBytes, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(netConfBytes, &n.NetConf); err != nil {
		return err
	}

	// Extract NetConf-specific fields
	if v, ok := raw["master"]; ok {
		if s, ok := v.(string); ok {
			n.Master = s
		}
	}
	if v, ok := raw["vlanMode"]; ok {
		if s, ok := v.(string); ok {
			n.VlanMode = s
		}
	}
	if v, ok := raw["vlanId"]; ok {
		if vlanIDFloat, ok := v.(float64); ok {
			vlanID := int(vlanIDFloat)
			n.VlanID = &vlanID
		}
	}
	if v, ok := raw["mtu"]; ok {
		if mtuFloat, ok := v.(float64); ok {
			n.MTU = int(mtuFloat)
		}
	}
	if v, ok := raw["linkInContainer"]; ok {
		if b, ok := v.(bool); ok {
			n.LinkContNs = b
		}
	}

	return nil
}

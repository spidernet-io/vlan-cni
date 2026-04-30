// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package vlan

import (
	"errors"
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/spidernet-io/vlan-cni/pkg/config"
)

// CmdAddStandard handles the CNI ADD command in standard mode
func CmdAddStandard(args *skel.CmdArgs, n *config.NetConf) (*current.Result, error) {
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	// Step 1: Create VLAN using config vlanId
	vlanIf, err := CreateVlan(n.Master, args.IfName, netns, *n.VlanID, n.MTU)
	if err != nil {
		return nil, fmt.Errorf("failed to create VLAN: %v", err)
	}

	// Step 2: Invoke IPAM
	r, err := ipam.ExecAdd(n.IPAM.Type, args.StdinData)
	if err != nil {
		DeleteVlan(args.IfName, netns)
		return nil, fmt.Errorf("IPAM failed: %v", err)
	}

	// Rollback function
	rollback := func() {
		ipam.ExecDel(n.IPAM.Type, args.StdinData)
		DeleteVlan(args.IfName, netns)
	}

	result, err := current.NewResultFromResult(r)
	if err != nil {
		rollback()
		return nil, err
	}

	if len(result.IPs) == 0 {
		rollback()
		return nil, errors.New("IPAM returned no IP configuration")
	}

	// Step 3: Configure IP
	for _, ipc := range result.IPs {
		ipc.Interface = current.Int(0)
	}
	result.Interfaces = []*current.Interface{vlanIf}

	if err := netns.Do(func(_ ns.NetNS) error {
		return ipam.ConfigureIface(args.IfName, result)
	}); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to configure IP: %v", err)
	}

	result.DNS = n.DNS
	return result, nil
}

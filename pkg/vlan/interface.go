// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package vlan

import (
	"fmt"
	"net"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

// CreateVlan creates a VLAN sub-interface with the specified parameters
func CreateVlan(master string, ifName string, netns ns.NetNS, vlanID int, mtu int) (*current.Interface, error) {
	vlan := &current.Interface{}

	// Get master interface
	m, err := netlink.LinkByName(master)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup master %q: %v", master, err)
	}

	// Generate temporary name to avoid conflicts
	tmpName, err := ip.RandomVethName()
	if err != nil {
		return nil, err
	}

	// Create VLAN link
	linkAttrs := netlink.NewLinkAttrs()
	if mtu > 0 {
		linkAttrs.MTU = mtu
	} else {
		linkAttrs.MTU = m.Attrs().MTU
	}
	linkAttrs.Name = tmpName
	linkAttrs.ParentIndex = m.Attrs().Index
	linkAttrs.Namespace = netlink.NsFd(int(netns.Fd()))

	v := &netlink.Vlan{
		LinkAttrs: linkAttrs,
		VlanId:    vlanID,
	}

	if err := netlink.LinkAdd(v); err != nil {
		return nil, fmt.Errorf("failed to create vlan: %v", err)
	}

	// Move to container namespace and rename
	err = netns.Do(func(_ ns.NetNS) error {
		if err := ip.RenameLink(tmpName, ifName); err != nil {
			return fmt.Errorf("failed to rename vlan to %q: %v", ifName, err)
		}
		vlan.Name = ifName

		// Re-fetch interface to get all properties
		contVlan, err := netlink.LinkByName(vlan.Name)
		if err != nil {
			return fmt.Errorf("failed to refetch vlan %q: %v", vlan.Name, err)
		}
		vlan.Mac = contVlan.Attrs().HardwareAddr.String()
		vlan.Sandbox = netns.Path()

		return nil
	})

	if err != nil {
		return nil, err
	}

	return vlan, nil
}

// DeleteVlan deletes the VLAN interface
func DeleteVlan(ifName string, netns ns.NetNS) error {
	return netns.Do(func(_ ns.NetNS) error {
		err := ip.DelLinkByName(ifName)
		if err != nil && err == ip.ErrLinkNotFound {
			return nil
		}
		return err
	})
}

// UpdateMac updates the MAC address of the interface
func UpdateMac(ifName string, macStr string) error {
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		return fmt.Errorf("invalid MAC address %q: %v", macStr, err)
	}

	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to find interface %q: %v", ifName, err)
	}

	if err := netlink.LinkSetHardwareAddr(link, mac); err != nil {
		return fmt.Errorf("failed to set MAC address: %v", err)
	}

	return nil
}

// GetMTU returns the MTU of the master interface
func GetMTU(master string) (int, error) {
	link, err := netlink.LinkByName(master)
	if err != nil {
		return 0, err
	}
	return link.Attrs().MTU, nil
}

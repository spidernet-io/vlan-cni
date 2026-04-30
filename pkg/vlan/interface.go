// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package vlan

import (
	"errors"
	"fmt"
	"net"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

// CreateVlan creates a VLAN sub-interface with the specified parameters
func CreateVlan(master string, ifName string, netns ns.NetNS, vlanID int, mtu int, mac string) (*current.Interface, error) {
	vlan := &current.Interface{}

	// Get master interface
	m, err := netlink.LinkByName(master)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup master %q: %w", master, err)
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
		return nil, fmt.Errorf("failed to create vlan: %w", err)
	}

	// Move to container namespace, rename and set MAC
	err = netns.Do(func(_ ns.NetNS) error {
		// Set MAC address if provided (before rename to avoid conflicts)
		if mac != "" {
			link, err := netlink.LinkByName(tmpName)
			if err != nil {
				return fmt.Errorf("failed to find vlan %q: %w", tmpName, err)
			}
			hwAddr, err := net.ParseMAC(mac)
			if err != nil {
				return fmt.Errorf("invalid MAC address %q: %w", mac, err)
			}
			if err := netlink.LinkSetHardwareAddr(link, hwAddr); err != nil {
				return fmt.Errorf("failed to set MAC address: %w", err)
			}
		}

		if err := ip.RenameLink(tmpName, ifName); err != nil {
			return fmt.Errorf("failed to rename vlan to %q: %w", ifName, err)
		}
		vlan.Name = ifName

		// Re-fetch interface to get all properties
		contVlan, err := netlink.LinkByName(vlan.Name)
		if err != nil {
			return fmt.Errorf("failed to refetch vlan %q: %w", vlan.Name, err)
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
		if errors.Is(err, ip.ErrLinkNotFound) {
			return nil
		}
		return err
	})
}

// UpdateMac updates the MAC address of the interface
func UpdateMac(ifName string, macStr string) error {
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		return fmt.Errorf("invalid MAC address %q: %w", macStr, err)
	}

	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to find interface %q: %w", ifName, err)
	}

	if err := netlink.LinkSetHardwareAddr(link, mac); err != nil {
		return fmt.Errorf("failed to set MAC address: %w", err)
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

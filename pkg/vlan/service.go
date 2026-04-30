// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package vlan

import (
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/spidernet-io/spiderpool/api/v1/agent/client/daemonset"
	"github.com/spidernet-io/spiderpool/api/v1/agent/models"
	spiderpoolopenapi "github.com/spidernet-io/spiderpool/pkg/openapi"

	"github.com/spidernet-io/vlan-cni/pkg/config"
)

// parseCNIArgs extracts K8S_POD_NAME and K8S_POD_NAMESPACE from CNI_ARGS
func parseCNIArgs(cniArgs string) (podName, podNamespace string, err error) {
	pairs := splitArgs(cniArgs)
	for _, pair := range pairs {
		kv := splitKV(pair)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "K8S_POD_NAME":
			podName = kv[1]
		case "K8S_POD_NAMESPACE":
			podNamespace = kv[1]
		}
	}
	if podName == "" || podNamespace == "" {
		return "", "", fmt.Errorf("K8S_POD_NAME and K8S_POD_NAMESPACE are required in CNI_ARGS")
	}
	return podName, podNamespace, nil
}

func splitArgs(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func splitKV(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

// CmdAddService handles the CNI ADD command in service mode.
// It queries spiderpool-agent via Unix socket for VLAN/MAC/IP assignment,
// then creates the VLAN interface and configures IPs directly (no IPAM call).
func CmdAddService(args *skel.CmdArgs, n *config.NetConf) (*current.Result, error) {
	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open netns %q: %w", args.Netns, err)
	}
	defer func() {
		_ = netns.Close()
	}()

	// Step 1: Parse K8S_POD_NAME and K8S_POD_NAMESPACE from CNI_ARGS
	podName, podNamespace, err := parseCNIArgs(args.Args)
	if err != nil {
		return nil, err
	}

	// Step 2: Query spiderpool-agent via Unix socket
	// Using the same pattern as spiderpool IPAM: openapi.NewAgentOpenAPIUnixClient
	client, err := spiderpoolopenapi.NewAgentOpenAPIUnixClient("")
	if err != nil {
		return nil, fmt.Errorf("failed to create spiderpool-agent client: %w", err)
	}

	// Create params for GetWorkloadendpoint (same pattern as spiderpool's PostIpamIP)
	params := daemonset.NewGetWorkloadendpointParams()
	params.PodName = podName
	params.PodNamespace = podNamespace

	resp, err := client.Daemonset.GetWorkloadendpoint(params)
	if err != nil {
		return nil, fmt.Errorf("GetWorkloadendpoint failed: %w", err)
	}

	// Step 3: Find the interface detail for the requested NIC
	ifaceDetail := findInterface(resp.Payload.Interfaces, args.IfName)
	if ifaceDetail == nil {
		return nil, fmt.Errorf("no assignment for nic %q in spiderpool-agent response", args.IfName)
	}

	// Step 4: Create VLAN using assignment
	mtu := n.MTU
	if mtu == 0 {
		mtu, _ = GetMTU(n.Master)
	}

	vlanIf, err := CreateVlan(n.Master, args.IfName, netns, int(ifaceDetail.Vlan), mtu, ifaceDetail.Mac)
	if err != nil {
		return nil, fmt.Errorf("failed to create VLAN: %w", err)
	}

	// Step 5: Build result from assignment IPs (no IPAM call needed)
	result := &current.Result{
		CNIVersion: n.CNIVersion,
		Interfaces: []*current.Interface{vlanIf},
		DNS:        n.DNS,
	}

	// Parse IPs from the assignment
	ips := getIPsFromInterface(ifaceDetail)
	for _, ipStr := range ips {
		ipAddr, ipNet, err := net.ParseCIDR(ipStr)
		if err != nil {
			return nil, fmt.Errorf("invalid IP from assignment: %s: %w", ipStr, err)
		}
		ipNet.IP = ipAddr
		result.IPs = append(result.IPs, &current.IPConfig{
			Interface: current.Int(0),
			Address:   *ipNet,
		})
	}

	if len(result.IPs) == 0 {
		return nil, fmt.Errorf("assignment contains no IPs for nic %q", args.IfName)
	}

	// Step 6: Configure IPs on the VLAN interface
	if err := netns.Do(func(_ ns.NetNS) error {
		return ipam.ConfigureIface(args.IfName, result)
	}); err != nil {
		return nil, fmt.Errorf("failed to configure IP: %w", err)
	}

	return result, nil
}

// findInterface finds the interface detail by name from the list
func findInterface(interfaces []*models.InterfaceDetail, ifName string) *models.InterfaceDetail {
	for _, iface := range interfaces {
		if iface.Interface != nil && *iface.Interface == ifName {
			return iface
		}
	}
	return nil
}

// getIPsFromInterface extracts all IPs from the interface detail
func getIPsFromInterface(iface *models.InterfaceDetail) []string {
	var ips []string
	if iface.IPV4 != "" {
		ips = append(ips, iface.IPV4)
	}
	if iface.IPV6 != "" {
		ips = append(ips, iface.IPV6)
	}
	return ips
}

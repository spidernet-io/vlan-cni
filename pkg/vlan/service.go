// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0
package vlan

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"

	"github.com/spidernet-io/spiderpool/api/v1/agent/client/daemonset"
	"github.com/spidernet-io/spiderpool/api/v1/agent/models"
	spiderpoolopenapi "github.com/spidernet-io/spiderpool/pkg/openapi"

	"github.com/spidernet-io/vlan-cni/pkg/config"
)

var unixSocketPath = "/var/run/spidernet/spiderpool.sock"

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
// Flow: IPAM alloc → GetWorkloadEndpoint (VLAN/MAC) → CreateVlan → ConfigureIface
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

	// Step 2: Invoke IPAM to allocate IP
	r, err := ipam.ExecAdd(n.IPAM.Type, args.StdinData)
	if err != nil {
		return nil, fmt.Errorf("IPAM failed: %w", err)
	}

	result, err := current.NewResultFromResult(r)
	if err != nil {
		return nil, err
	}

	// Step 3: Query spiderpool-agent via Unix socket to get VLAN ID and MAC
	// Using the same pattern as spiderpool IPAM: openapi.NewAgentOpenAPIUnixClient
	client, err := spiderpoolopenapi.NewAgentOpenAPIUnixClient(unixSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create spiderpool-agent client: %w", err)
	}

	params := daemonset.NewGetWorkloadendpointParams()
	params.PodName = podName
	params.PodNamespace = podNamespace

	resp, err := client.Daemonset.GetWorkloadendpoint(params)
	if err != nil {
		return nil, fmt.Errorf("GetWorkloadendpoint failed: %w", err)
	}

	// Step 4: Find the interface detail for the requested NIC
	ifaceDetail := findInterface(resp.Payload.Interfaces, args.IfName)
	if ifaceDetail == nil {
		return nil, fmt.Errorf("no assignment for nic %q in spiderpool-agent response", args.IfName)
	}

	// Step 5: Create VLAN interface using VLAN ID and MAC from spiderpool-agent
	mtu := n.MTU
	if mtu == 0 {
		mtu, _ = GetMTU(n.Master)
	}

	vlanIf, err := CreateVlan(n.Master, args.IfName, netns, int(ifaceDetail.Vlan), mtu, ifaceDetail.Mac)
	if err != nil {
		return nil, fmt.Errorf("failed to create VLAN: %w", err)
	}

	// Step 6: Configure IPs (from IPAM result) on the VLAN interface
	for _, ipc := range result.IPs {
		ipc.Interface = current.Int(0)
	}
	result.Interfaces = []*current.Interface{vlanIf}
	result.DNS = n.DNS

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

# VLAN CNI with External VLAN Service Specification

Enable vlan-cni to retrieve VLAN configuration from spiderpool-agent, supporting both standard (static config) and service (dynamic allocation) deployment modes.

## Overview

This CNI plugin extends [community VLAN CNI](https://github.com/containernetworking/plugins/tree/main/plugins/main/vlan) with:

- **Service-driven VLAN allocation** - Query spiderpool-agent via Unix socket for VLAN ID, MAC, and IP assignments
- **Dual execution modes** - Automatic mode selection based on whether `vlanId` is present in configuration
- **Backward compatible** - Existing configs with `vlanId` work without changes (standard mode)

### Mode Selection

The mode is determined by whether `vlanId` is present in the CNI JSON configuration:

- **`vlanId` present** (including `"vlanId": 0`) → **Standard Mode**: use the configured VLAN ID directly
- **`vlanId` absent** → **Service Mode**: call spiderpool-agent `GetWorkloadEndpoint` to obtain VLAN ID, MAC, and IPs

> **Design Note**: `VlanID` uses `*int` (pointer) type in Go to distinguish between "not configured" (`nil`) and "configured as 0" (`&0`). VLAN ID 0 is valid in IEEE 802.1Q (priority tagging), so we cannot use the zero value to mean "unset".

### Execution Flow

```
+------------------+      +------------------+
|    CNI ADD       |      |    CNI ADD       |
+--------+---------+      +--------+---------+
         |                         |
         v                         v
+--------+---------+      +--------+---------+
| Load Configuration|      | Load Configuration|
+--------+---------+      +--------+---------+
         |                         | 
          -------------------------  
                     |         
                     v              
            +------------------+
            | vlanId present   |
            | in config?       |
            +--------+---------+
                     |         
                     v   
          -------------------------
         | Yes                     | No (nil)
         v                         v
+------------------+      +--------+---------+
| Standard Mode    |      | Invoke IPAM      |
|                  |      | allocate IP      |
| 1. Create VLAN   |      +--------+---------+
|    (config vlanId)|               |
| 2. Invoke IPAM   |               v
| 3. Configure IP   |      +--------+---------+
+--------+---------+      | Call Service API |
         |                | POST {IP}        |
         |                +--------+---------+
         |                         |
         |                         v
         |                +--------+---------+
         |                | Create VLAN      |
         |                | (service vlanId) |
         |                +--------+---------+
         |                         |
         v                         v
+------------------+      +--------+---------+
|   Return Result  |      |   Return Result  |
+------------------+      +------------------+

[Error Cases]
- Config validation fails  -> Error
- IPAM fails             -> Rollback + Error
- Service call fails      -> Rollback + Error
- VLAN creation fails    -> Rollback + Error
- IP config fails         -> Rollback + Error
```

### Mode 1: Standard Mode (vlanId present in config)

Use when VLAN information is statically configured.

**Flow**:
```
1. Create VLAN sub-interface using config vlanId
2. Invoke IPAM to allocate IP
3. Configure IP on VLAN interface
```

### Mode 2: Service Mode (vlanId absent in config)

Use when VLAN information is dynamically allocated by external service (e.g., cloud IaaS).
The vlan-cni connects to spiderpool-agent via Unix socket (`/var/run/spidernet/spiderpool.sock`) and calls `GetWorkloadEndpoint`.

**Flow**:
```
1. Connect to spiderpool-agent Unix socket
2. Call GetWorkloadEndpoint(podName, podNamespace, nic) to get VLAN/MAC/IPs
3. Create VLAN sub-interface using response
4. Configure IP on VLAN interface (using IPs from response, skip IPAM)
```

## Configuration

### NetConf Structure

```go
type NetConf struct {
    types.NetConf
    Master     string `json:"master"`                    // Master interface name (required)
    VlanID     *int   `json:"vlanId,omitempty"`          // VLAN ID (0-4094). nil = service mode, non-nil = standard mode
    MTU        int    `json:"mtu,omitempty"`
    LinkContNs bool   `json:"linkInContainer,omitempty"`
}
```

> **Note**: Service mode does not require any additional configuration field. The vlan-cni automatically connects to the spiderpool-agent Unix socket at a well-known path.

### Configuration Examples

**Standard Mode** (vlanId present → static VLAN):
```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0",
  "vlanId": 100,
  "ipam": {
    "type": "spiderpool"
  }
}
```

**Standard Mode with Priority Tagging** (vlanId = 0):
```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0",
  "vlanId": 0,
  "ipam": {
    "type": "spiderpool"
  }
}
```

**Service Mode** (vlanId absent → dynamic VLAN from cloud IaaS):
```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0",
  "ipam": {
    "type": "spiderpool"
  }
}
```

## Spiderpool-Agent API: GetWorkloadEndpoint

In service mode, vlan-cni queries spiderpool-agent via its Unix socket, following the same client pattern as the spiderpool IPAM plugin (`cmd/spiderpool`).

### Connection

- **Socket**: `/var/run/spidernet/spiderpool.sock` (well-known path, same as spiderpool IPAM)
- **Client**: reuse spiderpool's `NewAgentOpenAPIUnixClient` or equivalent Unix socket HTTP client

### RPC: GetWorkloadEndpoint

**Parameters**:

| Parameter | Required | Source | Description |
|-----------|----------|--------|-------------|
| `podName` | Yes | `CNI_ARGS` (`K8S_POD_NAME`) | Pod name |
| `podNamespace` | Yes | `CNI_ARGS` (`K8S_POD_NAMESPACE`) | Pod namespace |
| `nic` | No | `CNI_IFNAME` | If specified, return only this NIC's data |

### Response

Returns `WorkloadEndpointStatus` with interfaces array:

```json
{
  "podName": "my-pod",
  "podNamespace": "default",
  "podUID": "...",
  "node": "node-1",
  "interfaces": [
    {
      "interface": "net1",
      "ipv4": "192.168.1.100/24",
      "ipv4Gateway": "192.168.1.1",
      "ipv6": "fd00::100/64",
      "vlan": 100,
      "mac": "aa:bb:cc:dd:ee:ff"
    },
    {
      "interface": "net2",
      "ipv4": "192.168.2.100/24",
      "vlan": 200,
      "mac": "aa:bb:cc:dd:ee:00"
    }
  ]
}
```

The client finds the matching interface by `interface` name.
## Execution Flow

### cmdAdd Flow

```
1. Load configuration
   - Validate master interface is specified
   - Determine mode: standard (VlanID != nil) or service (VlanID == nil)
   - In standard mode: validate *VlanID is in range (0-4094)

2. Open network namespace

3. IF service mode (VlanID == nil):
     a. Parse K8S_POD_NAME and K8S_POD_NAMESPACE from CNI_ARGS

     b. Connect to spiderpool-agent Unix socket
        // Same pattern as spiderpool IPAM:
        // spiderpoolAgentAPI, err := openapi.NewAgentOpenAPIUnixClient(socketPath)
        client, err := NewAgentOpenAPIUnixClient("/var/run/spidernet/spiderpool.sock")

     c. Call GetWorkloadEndpoint (similar to spiderpool's PostIpamIP)
        params := &GetWorkloadEndpointParams{
            PodName: podName, PodNamespace: podNamespace, Nic: args.IfName,
        }
        resp, err := client.Daemonset().GetWorkloadEndpoint(params)
        if err != nil {
            return error("GetWorkloadEndpoint failed: ...")
        }
        assignment := resp.Payload.IPAssignments[args.IfName]
     
     d. Create VLAN sub-interface (VLAN and MAC set in one call)
        vlanIf = createVlan(master, ifName, assignment.VlanId, assignment.MAC)
     
     e. Configure IP on VLAN interface (using IPs from assignment, no IPAM call)
        configureIPs(ifName, assignment.IPs)

4. IF standard mode (VlanID != nil):
     a. Create VLAN sub-interface using config vlanId
        vlanIf = createVlan(master, ifName, *n.VlanID)
     
     b. Invoke IPAM
        result = ipam.ExecAdd(n.IPAM.Type, args.StdinData)
        if err != nil {
            deleteVlan(ifName)  // Cleanup
            return error("IPAM failed: ...")
        }
     
     c. Configure IP on VLAN interface
        ipam.ConfigureIface(ifName, result)

5. Return result with interface info
```

### Mode Selection Logic

| VlanID in JSON | VlanID Go value | Mode | Validation |
|----------------|-----------------|------|------------|
| `"vlanId": 100` | `*int = &100` | Standard | Validate 0-4094 |
| `"vlanId": 0`   | `*int = &0`   | Standard | OK (priority tagging) |
| field absent     | `*int = nil`  | Service  | Connect to spiderpool-agent socket |

## Key Implementation Points

### 1. Configuration Validation

```go
func loadConf(args *skel.CmdArgs) (*NetConf, string, error) {
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
    } else {
        // Service mode: will connect to spiderpool-agent Unix socket
        // No additional config validation needed
    }
    
    return n, n.CNIVersion, nil
}
```

### 2. Spiderpool-Agent Client

vlan-cni directly uses spiderpool's generated OpenAPI client (same pattern as spiderpool IPAM):

```go
import (
    "github.com/spidernet-io/spiderpool/api/v1/agent/client/daemonset"
    "github.com/spidernet-io/spiderpool/api/v1/agent/models"
    spiderpoolopenapi "github.com/spidernet-io/spiderpool/pkg/openapi"
)

// Create Unix socket client (same pattern as spiderpool IPAM)
client, err := spiderpoolopenapi.NewAgentOpenAPIUnixClient("")
if err != nil {
    return fmt.Errorf("failed to create spiderpool-agent client: %v", err)
}

// Build parameters
params := daemonset.NewGetWorkloadendpointParams()
params.PodName = podName
params.PodNamespace = podNamespace

// Call GetWorkloadendpoint (similar to spiderpool's PostIpamIP)
resp, err := client.Daemonset.GetWorkloadendpoint(params)
if err != nil {
    return fmt.Errorf("GetWorkloadendpoint failed: %v", err)
}

// Find interface by name
var ifaceDetail *models.InterfaceDetail
for _, iface := range resp.Payload.Interfaces {
    if iface.Interface != nil && *iface.Interface == ifName {
        ifaceDetail = iface
        break
    }
}

// Extract data
vlanId := int(ifaceDetail.Vlan)
mac := ifaceDetail.Mac
ips := []string{}
if ifaceDetail.IPV4 != "" {
    ips = append(ips, ifaceDetail.IPV4)
}
if ifaceDetail.IPV6 != "" {
    ips = append(ips, ifaceDetail.IPV6)
}
```

**Key Types** (from spiderpool generated code):
- `models.WorkloadEndpointStatus`: Response payload with interfaces array
- `models.InterfaceDetail`: Per-interface data (name, IPs, VLAN, MAC, routes)
- `daemonset.GetWorkloadendpointParams`: Query parameters (PodName, PodNamespace)

## Error Handling

| Scenario | Mode | Error Message | Rollback |
|----------|------|---------------|----------|
| Spiderpool-agent socket unreachable | Service | `failed to connect to spiderpool-agent: ...` | N/A |
| GetWorkloadEndpoint error | Service | `GetWorkloadEndpoint failed: ...` | N/A |
| NIC not found in response | Service | `no assignment for nic "X"` | N/A |
| Invalid vlanId from service | Service | `invalid vlanId from service: X` | N/A |
| IPAM failure | Standard | `IPAM failed: ...` | Cleanup VLAN (if created) |
| VLAN creation failure | Both | `failed to create VLAN: ...` | Release IPAM (standard) |
| MAC update failure | Service | `failed to set MAC: ...` | Delete VLAN |
| IP configuration failure | Both | Error from configureIface | Full rollback |

## Compatibility

- **Backward Compatible**: Existing standard configs with `vlanId` work without changes
- **Automatic Mode Selection**: Mode determined by presence/absence of `vlanId` in config
- **Zero-Value Safe**: `"vlanId": 0` is valid (priority tagging), only missing field triggers service mode
- **IPAM Agnostic**: Standard mode works with any CNI IPAM plugin
- **Spiderpool Integration**: Service mode reuses spiderpool-agent Unix socket (no extra config needed)

## Implementation File Structure

```
vlan-cni/
├── cmd/
│   └── vlan/
│       └── main.go              # Main entry
├── pkg/
│   ├── config/
│   │   └── config.go            # NetConf definition
│   └── vlan/
│       ├── interface.go         # VLAN create/delete (uses spiderpool client)
│       ├── standard.go          # Standard mode implementation
│       └── service.go           # Service mode implementation
├── go.mod                       # Depends on github.com/spidernet-io/spiderpool
└── README.md
```

## Test Scenarios

### Standard Mode
1. **Normal**: Config with vlanId, IPAM allocates IP → Create VLAN → Config IP
2. **Missing vlanId**: Config without vlanId → Error
3. **IPAM failure**: IPAM fails → VLAN cleaned up, error returned

### Service Mode
4. **Normal**: GetWorkloadEndpoint returns vlanId+MAC+IPs → Create VLAN with MAC → Config IP
5. **Agent unreachable**: Socket connection fails → Error (no cleanup needed)
6. **NIC not in response**: GetWorkloadEndpoint returns no entry for NIC → Error
7. **Invalid service response**: Response contains invalid vlanId → Error
8. **VLAN creation failure**: Got assignment but VLAN creation fails → Error
9. **MAC update failure**: MAC update fails → Delete VLAN, error

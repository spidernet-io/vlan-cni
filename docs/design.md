# VLAN CNI with External VLAN Service Specification

Enable vlan-cni to retrieve VLAN configuration from an external HTTP service, supporting both standard (static config) and cloud (dynamic service) deployment modes.

## Overview

This CNI plugin extends [community VLAN CNI](https://github.com/containernetworking/plugins/tree/main/plugins/main/vlan) with:

- **Service-driven VLAN allocation** - Query external HTTP API for VLAN ID and MAC address
- **Dual execution modes** - Automatic mode selection based on configuration
- **Backward compatible** - Works without service URL (standard mode)

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
            | externalService  |
            | URL set?         |
            +--------+---------+
                     |         
                     v   
          -------------------------
         | No                      | Yes
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

### Mode 1: Standard Mode (externalServiceURL not set)

Use when VLAN information is statically configured.

**Flow**:
```
1. Create VLAN sub-interface using config vlanId
2. Invoke IPAM to allocate IP
3. Configure IP on VLAN interface
```

### Mode 2: Service Mode (externalServiceURL is set)

Use when VLAN information is dynamically allocated by external service (e.g., cloud IaaS).

**Flow**:
```
1. Invoke IPAM to allocate IP
2. Query externalServiceURL to get VLAN ID and MAC
3. Create VLAN sub-interface using service response
4. Configure IP on VLAN interface
```

## Configuration

### NetConf Structure

```go
type NetConf struct {
    types.NetConf
    Master          string `json:"master"`                     // Master interface name (required)
    VlanID          int    `json:"vlanId,omitempty"`           // VLAN ID (1-4094, required in standard mode)
    MTU             int    `json:"mtu,omitempty"`
    LinkContNs      bool   `json:"linkInContainer,omitempty"`
    ExternalServiceURL  string `json:"externalServiceURL,omitempty"`  
}
```

### Configuration Examples

**Standard Mode** (static VLAN):
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

**Service Mode** (dynamic VLAN from cloud IaaS):
```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0",
  "externalServiceURL": "http://iaas-api:8080/v1/network/vlan",
  "ipam": {
    "type": "spiderpool"
  }
}
```

## External Service API

### Request

**Method**: POST
**Headers**:
```
Content-Type: application/json
```

**Body**:
```json
{
  "allocatedIP": "192.168.1.10/24"
}
```

### Response

**Success (200)**:
```json
{
  "vlanId": 100,                    // Required: VLAN ID (1-4094)
  "mac": "aa:bb:cc:dd:ee:ff",        // Optional: MAC address
}
```

**Error**:
```json
{
  "error": "No available entry for VLAN",
}
```

## Execution Flow

### cmdAdd Flow

```
1. Load configuration
   - Validate master interface is specified
   - Determine mode: standard (externalServiceURL="") or service (externalServiceURL!="")
   - In standard mode: validate vlanId is provided (1-4094)

2. Open network namespace

3. IF service mode (externalServiceURL != ""):
     a. Invoke IPAM first
        result = ipam.ExecAdd(n.IPAM.Type, args.StdinData)
        if err != nil {
            return error("IPAM failed: ...")
        }
     
     b. Query VLAN service
        vlanInfo = queryVlanService(n.externalServiceURL, args, result.IPs)
        if err != nil {
            ipam.ExecDel(n.IPAM.Type, args.StdinData)  // Rollback IPAM
            return error("VLAN service failed: ...")
        }
     
     c. Create VLAN sub-interface
        vlanIf = createVlan(master, ifName, vlanInfo.VlanId)
        if vlanInfo.MAC != "" {
            updateMac(ifName, vlanInfo.MAC)
        }
     
     d. Configure IP on VLAN interface
        ipam.ConfigureIface(ifName, result)

4. IF standard mode (externalServiceURL == ""):
     a. Create VLAN sub-interface using config vlanId
        vlanIf = createVlan(master, ifName, n.VlanID)
     
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

| externalServiceURL | VlanID (config) | Mode | Validation |
|----------------|-----------------|------|------------|
| "" (empty)     | >= 0            | Standard | OK |
| set (non-empty)| any             | Service | OK (vlanId from service) |

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
    
    // Standard mode: vlanId is required
    if n.externalServiceURL == "" {
        if n.VlanID <= 0 || n.VlanID > 4094 {
            return nil, "", fmt.Errorf("\"vlanId\" is required (1-4094) in standard mode")
        }
    }
    
    // Validate vlanId range if provided
    if n.VlanID < 0 || n.VlanID > 4094 {
        return nil, "", fmt.Errorf("invalid VLAN ID %d (must be 1-4094)", n.VlanID)
    }
    
    return n, n.CNIVersion, nil
}
```

### 2. VLAN Service Client

```go
type VlanServiceRequest struct {
    AllocatedIP string `json:"allocatedIP"`
}

type VlanServiceResponse struct {
    VlanID int    `json:"vlanId"`
    MAC    string `json:"mac,omitempty"`
}

func queryVlanService(serviceURL string, req *VlanServiceRequest) (*VlanServiceResponse, error) {
    // TODO: implement VLAN service query logic
    return nil, nil
}
```

## Error Handling

| Scenario | Mode | Error Message | Rollback |
|----------|------|---------------|----------|
| Standard mode missing vlanId | Standard | `vlanId is required (1-4094) in standard mode` | N/A |
| IPAM failure | Both | `IPAM failed: ...` | Cleanup VLAN (if created) |
| VLAN service unreachable | Service | `failed to call VLAN service: ...` | Release IPAM |
| VLAN service error response | Service | `VLAN service returned X: ...` | Release IPAM |
| Invalid vlanId from service | Service | `invalid vlanId from service: X` | Release IPAM |
| VLAN creation failure | Both | `failed to create VLAN: ...` | Release IPAM |
| MAC update failure | Service | `failed to set MAC: ...` | Release IPAM + delete VLAN |
| IP configuration failure | Both | Error from ipam.ConfigureIface | Full rollback |

## Compatibility

- **Backward Compatible**: Existing standard configs work without changes
- **Optional Service**: Service mode only activated when `externalServiceURL` is set
- **IPAM Agnostic**: Works with any CNI IPAM plugin

## Implementation File Structure

```
vlan-cni/
├── cmd/
│   └── vlan/
│       └── main.go              # Main entry
├── pkg/
│   ├── config/
│   │   └── config.go            # NetConf definition
│   ├── service/
│   │   ├── client.go            # HTTP client for VLAN service
│   │   └── types.go             # Request/Response types
│   └── vlan/
│       ├── interface.go         # VLAN create/delete/update
│       ├── standard.go          # Standard mode implementation
│       └── service.go           # Service mode implementation
├── go.mod
└── README.md
```

## Test Scenarios

### Standard Mode
1. **Normal**: Config with vlanId, IPAM allocates IP → Create VLAN → Config IP
2. **Missing vlanId**: Config without vlanId → Error
3. **IPAM failure**: IPAM fails → VLAN cleaned up, error returned

### Service Mode
4. **Normal**: Service returns vlanId+MAC → Create VLAN with MAC → Config IP
5. **Service unavailable**: IPAM succeeds, service fails → IPAM released
6. **Invalid service response**: Service returns invalid vlanId → IPAM released
7. **VLAN creation failure**: IPAM+service succeed, VLAN fails → Full rollback
8. **MAC update failure**: MAC update fails → Full rollback

# vlan-cni

[![CI](https://github.com/spidernet-io/vlan-cni/actions/workflows/ci.yml/badge.svg)](https://github.com/spidernet-io/vlan-cni/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/spidernet-io/vlan-cni)](https://goreportcard.com/report/github.com/spidernet-io/vlan-cni)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A CNI plugin that extends the [community VLAN CNI](https://github.com/containernetworking/plugins/tree/main/plugins/main/vlan) with dynamic VLAN/MAC/IP assignment via [spiderpool-agent](https://github.com/spidernet-io/spiderpool).

## What is vlan-cni?

vlan-cni is a CNI plugin for Kubernetes that creates VLAN sub-interfaces inside Pod network namespaces. It supports two operating modes:

- **Standard mode** — statically configure a VLAN ID in the CNI config, identical to the upstream community plugin behavior.
- **Service mode** — dynamically retrieve VLAN ID, MAC address, and IP assignments from spiderpool-agent at Pod creation time, enabling cloud IaaS-driven network allocation without any static config.

## Key Features

- **Dual mode** — auto-selects standard or service mode based on whether `vlanId` is present in the CNI config; no extra flag needed.
- **Dynamic VLAN allocation** — in service mode, queries spiderpool-agent via Unix socket (`/var/run/spidernet/spiderpool.sock`) using spiderpool's official OpenAPI client.
- **MAC address assignment** — in service mode, the MAC address returned by spiderpool-agent is applied directly to the VLAN interface at creation time.
- **Backward compatible** — all existing CNI configs that include `vlanId` continue to work unchanged.
- **Zero-value safe** — `"vlanId": 0` (IEEE 802.1Q priority tagging) is treated as standard mode, not service mode.

## Differences from Community VLAN CNI

| Feature | Community VLAN CNI | vlan-cni |
|---|---|---|
| VLAN ID source | Static config only | Static config **or** spiderpool-agent (dynamic) |
| MAC address | Not managed | Set from spiderpool-agent response in service mode |
| IP assignment | Delegated to IPAM plugin | IPAM plugin (standard) or spiderpool-agent response (service) |
| spiderpool integration | None | Native Unix socket client via `GetWorkloadEndpoint` RPC |
| Mode selection | N/A | Auto-detected from `vlanId` presence |

## How It Works

### Mode Selection

| CNI Config | Go Value | Mode |
|---|---|---|
| `"vlanId": 100` | `*int = &100` | Standard |
| `"vlanId": 0` | `*int = &0` | Standard (priority tagging) |
| field absent | `*int = nil` | Service |

### Standard Mode Flow

```
1. Create VLAN sub-interface using vlanId from config
2. Invoke IPAM plugin to allocate IP
3. Configure IP on VLAN interface
```

### Service Mode Flow

```
1. Parse K8S_POD_NAME and K8S_POD_NAMESPACE from CNI_ARGS
2. Connect to spiderpool-agent Unix socket
3. Call GetWorkloadEndpoint(podName, podNamespace) → VLAN ID, MAC, IPs
4. Create VLAN sub-interface and set MAC (in one step)
5. Configure IPs directly (no IPAM call)
```

## Configuration

### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `master` | string | Yes | Host network interface to attach the VLAN to |
| `vlanId` | int | No | VLAN ID (0–4094). Absent = service mode |
| `mtu` | int | No | MTU for the VLAN interface. Defaults to master MTU |
| `linkInContainer` | bool | No | Whether the master link is in the container namespace |
| `ipam` | object | Standard mode | IPAM plugin config (not used in service mode) |

### Standard Mode

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

### Service Mode

```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0"
}
```

> In service mode, spiderpool-agent must be running and its Unix socket must be accessible at `/var/run/spidernet/spiderpool.sock`.

## Requirements

- Linux kernel with 802.1Q VLAN support
- Go 1.22+
- [spiderpool](https://github.com/spidernet-io/spiderpool) deployed in the cluster (service mode only)

## Building

```bash
# Clone the repository
git clone https://github.com/spidernet-io/vlan-cni.git
cd vlan-cni

# Download dependencies
make deps

# Build the binary (outputs to ./bin/vlan)
make build

# Cross-compile for Linux amd64
GOOS=linux GOARCH=amd64 go build -o bin/vlan ./cmd/vlan
```

The compiled binary `bin/vlan` is the CNI plugin. Copy it to the CNI bin directory on each node (typically `/opt/cni/bin/`).

## Project Structure

```
vlan-cni/
├── cmd/vlan/          # Plugin entry point (main.go)
├── pkg/
│   ├── config/        # NetConf definition and validation
│   └── vlan/
│       ├── interface.go   # VLAN create / delete / MAC update
│       ├── standard.go    # Standard mode implementation
│       └── service.go     # Service mode implementation
├── docs/
│   └── design.md      # Detailed design specification
├── Makefile
└── go.mod
```

## Documentation

See [docs/design.md](docs/design.md) for the full design specification, including detailed flow diagrams, API reference for `GetWorkloadEndpoint`, error handling table, and implementation notes.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
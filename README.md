# vlan-cni

[![CI](https://github.com/spidernet-io/vlan-cni/actions/workflows/ci.yml/badge.svg)](https://github.com/spidernet-io/vlan-cni/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/spidernet-io/vlan-cni)](https://goreportcard.com/report/github.com/spidernet-io/vlan-cni)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A CNI plugin that extends the [community VLAN CNI](https://github.com/containernetworking/plugins/tree/main/plugins/main/vlan) with dynamic VLAN/MAC/IP assignment via [spiderpool-agent](https://github.com/spidernet-io/spiderpool).

## What is vlan-cni?

vlan-cni is a CNI plugin for Kubernetes that creates VLAN sub-interfaces inside Pod network namespaces. It supports two operating modes:

- **Manual mode** — statically configure a VLAN ID in the CNI config. If `vlanId` is omitted, it defaults to 0.
- **Auto mode** — dynamically retrieve VLAN ID and MAC address from spiderpool-agent after IPAM allocation.

## Key Features

- **Dual mode** — use `vlanMode: manual` or `vlanMode: auto`; omitted `vlanMode` keeps legacy mode selection by `vlanId` presence.
- **Dynamic VLAN allocation** — in auto mode, queries spiderpool-agent via Unix socket (`/var/run/spidernet/spiderpool.sock`) using spiderpool's official OpenAPI client.
- **MAC address assignment** — in auto mode, the MAC address returned by spiderpool-agent is applied directly to the VLAN interface at creation time.
- **Backward compatible** — all existing CNI configs that include `vlanId` continue to work unchanged.
- **Zero-value safe** — `"vlanId": 0` (IEEE 802.1Q priority tagging) is treated as manual mode, not auto mode.

## Differences from Community VLAN CNI

| Feature | Community VLAN CNI | vlan-cni |
|---|---|---|
| VLAN ID source | Static config only | Static config **or** spiderpool-agent (dynamic) |
| MAC address | Not managed | Set from spiderpool-agent response in auto mode |
| IP assignment | Delegated to IPAM plugin | Delegated to IPAM plugin |
| spiderpool integration | None | Native Unix socket client via `GetWorkloadEndpoint` RPC |
| Mode selection | N/A | Explicit `vlanMode`, with legacy detection from `vlanId` presence |

## How It Works

### Mode Selection

| CNI Config | Effective Mode | VLAN Source |
|---|---|---|
| `"vlanMode": "manual", "vlanId": 100` | Manual | Configured `vlanId` |
| `"vlanMode": "manual"` | Manual | Default `vlanId` 0 |
| `"vlanMode": "auto"` | Auto | IPAM/spiderpool-agent |
| `"vlanId": 100` | Manual (legacy) | Configured `vlanId` |
| field absent | Auto (legacy) | IPAM/spiderpool-agent |

### Manual Mode Flow

```
1. Create VLAN sub-interface using vlanId from config
2. Invoke IPAM plugin to allocate IP
3. Configure IP on VLAN interface
```

### Auto Mode Flow

```
1. Parse K8S_POD_NAME and K8S_POD_NAMESPACE from CNI_ARGS
2. Invoke IPAM plugin to allocate IP
3. Connect to spiderpool-agent Unix socket
4. Call GetWorkloadEndpoint(podName, podNamespace) → VLAN ID, MAC
5. Create VLAN sub-interface and set MAC
6. Configure IP from IPAM result on VLAN interface
```

## Configuration

### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `master` | string | Yes | Host network interface to attach the VLAN to |
| `vlanMode` | string | No | VLAN mode: `manual` or `auto`. If omitted, legacy mode detection is used |
| `vlanId` | int | No | VLAN ID (0-4094). Effective in `manual` mode, defaults to 0 |
| `mtu` | int | No | MTU for the VLAN interface. Defaults to master MTU |
| `linkInContainer` | bool | No | Whether the master link is in the container namespace |
| `ipam` | object | Yes | IPAM plugin config |

When `vlanMode` is omitted, existing behavior is preserved: configs with `vlanId` use manual mode, and configs without `vlanId` use auto mode.

### Manual Mode

```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0",
  "vlanMode": "manual",
  "vlanId": 100,
  "ipam": {
    "type": "spiderpool"
  }
}
```

### Auto Mode

```json
{
  "cniVersion": "1.0.0",
  "name": "vlan-network",
  "type": "vlan",
  "master": "eth0",
  "vlanMode": "auto",
  "ipam": {
    "type": "spiderpool"
  }
}
```

> In auto mode, vlan-cni dynamically gets VLAN information via IPAM/spiderpool-agent. spiderpool-agent must be running and its Unix socket must be accessible at `/var/run/spidernet/spiderpool.sock`.

## Requirements

- Linux kernel with 802.1Q VLAN support
- Go 1.22+
- [spiderpool](https://github.com/spidernet-io/spiderpool) deployed in the cluster (auto mode only)

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

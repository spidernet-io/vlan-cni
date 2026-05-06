// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"encoding/json"

	"github.com/containernetworking/cni/pkg/skel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spidernet-io/vlan-cni/pkg/config"
)

func intPtr(i int) *int {
	return &i
}

var _ = Describe("Config Loading", func() {
	Context("standard mode configuration", func() {
		It("should load valid standard mode config", func() {
			conf := `{
				"cniVersion": "1.0.0",
				"name": "vlan-network",
				"type": "vlan",
				"master": "eth0",
				"vlanId": 100,
				"ipam": {
					"type": "spiderpool"
				}
			}`

			args := &skel.CmdArgs{
				StdinData: []byte(conf),
			}

			netConf, cniVersion, err := config.LoadConf(args)
			Expect(err).NotTo(HaveOccurred())
			Expect(cniVersion).To(Equal("1.0.0"))
			Expect(netConf.Master).To(Equal("eth0"))
			Expect(netConf.VlanID).NotTo(BeNil())
			Expect(*netConf.VlanID).To(Equal(100))
			Expect(netConf.IsServiceMode()).To(BeFalse())
		})

		It("should accept vlanId 0 (priority tagging) as standard mode", func() {
			conf := `{
				"cniVersion": "1.0.0",
				"name": "vlan-network",
				"type": "vlan",
				"master": "eth0",
				"vlanId": 0,
				"ipam": {
					"type": "spiderpool"
				}
			}`

			args := &skel.CmdArgs{
				StdinData: []byte(conf),
			}

			netConf, _, err := config.LoadConf(args)
			Expect(err).NotTo(HaveOccurred())
			Expect(netConf.VlanID).NotTo(BeNil())
			Expect(*netConf.VlanID).To(Equal(0))
			Expect(netConf.IsServiceMode()).To(BeFalse())
		})

		It("should reject invalid vlanId range", func() {
			conf := `{
				"cniVersion": "1.0.0",
				"name": "vlan-network",
				"type": "vlan",
				"master": "eth0",
				"vlanId": 5000,
				"ipam": {
					"type": "spiderpool"
				}
			}`

			args := &skel.CmdArgs{
				StdinData: []byte(conf),
			}

			_, _, err := config.LoadConf(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid vlanId"))
		})

		It("should reject negative vlanId", func() {
			conf := `{
				"cniVersion": "1.0.0",
				"name": "vlan-network",
				"type": "vlan",
				"master": "eth0",
				"vlanId": -1,
				"ipam": {
					"type": "spiderpool"
				}
			}`

			args := &skel.CmdArgs{
				StdinData: []byte(conf),
			}

			_, _, err := config.LoadConf(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid vlanId"))
		})
	})

	Context("service mode configuration", func() {
		It("should load valid service mode config (vlanId absent)", func() {
			conf := `{
				"cniVersion": "1.0.0",
				"name": "vlan-network",
				"type": "vlan",
				"master": "eth0",
				"ipam": {
					"type": "spiderpool"
				}
			}`

			args := &skel.CmdArgs{
				StdinData: []byte(conf),
			}

			netConf, cniVersion, err := config.LoadConf(args)
			Expect(err).NotTo(HaveOccurred())
			Expect(cniVersion).To(Equal("1.0.0"))
			Expect(netConf.Master).To(Equal("eth0"))
			Expect(netConf.VlanID).To(BeNil())
			Expect(netConf.IsServiceMode()).To(BeTrue())
		})
	})

	Context("common validation", func() {
		It("should reject config without master", func() {
			conf := `{
				"cniVersion": "1.0.0",
				"name": "vlan-network",
				"type": "vlan",
				"vlanId": 100,
				"ipam": {
					"type": "spiderpool"
				}
			}`

			args := &skel.CmdArgs{
				StdinData: []byte(conf),
			}

			_, _, err := config.LoadConf(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("master"))
			Expect(err.Error()).To(ContainSubstring("required"))
		})
	})
})

var _ = Describe("NetConf Mode Detection", func() {
	It("should detect service mode when VlanID is nil", func() {
		netConf := &config.NetConf{}
		Expect(netConf.IsServiceMode()).To(BeTrue())
	})

	It("should detect standard mode when VlanID is set", func() {
		netConf := &config.NetConf{}
		netConf.VlanID = intPtr(100)
		Expect(netConf.IsServiceMode()).To(BeFalse())
	})

	It("should detect standard mode when VlanID is 0", func() {
		netConf := &config.NetConf{}
		netConf.VlanID = intPtr(0)
		Expect(netConf.IsServiceMode()).To(BeFalse())
	})
})

var _ = Describe("NetConf JSON Serialization", func() {
	It("should marshal and unmarshal correctly with vlanId", func() {
		original := &config.NetConf{}
		original.Master = "eth0"
		original.VlanID = intPtr(100)
		original.MTU = 1500
		original.LinkContNs = true

		data, err := json.Marshal(original)
		Expect(err).NotTo(HaveOccurred())

		var parsed config.NetConf
		err = json.Unmarshal(data, &parsed)
		Expect(err).NotTo(HaveOccurred())

		Expect(parsed.Master).To(Equal("eth0"))
		Expect(parsed.VlanID).NotTo(BeNil())
		Expect(*parsed.VlanID).To(Equal(100))
		Expect(parsed.MTU).To(Equal(1500))
		Expect(parsed.LinkContNs).To(BeTrue())
	})

	It("should unmarshal to nil VlanID when field absent", func() {
		data := []byte(`{"master": "eth0"}`)

		var parsed config.NetConf
		err := json.Unmarshal(data, &parsed)
		Expect(err).NotTo(HaveOccurred())

		Expect(parsed.VlanID).To(BeNil())
	})

	It("should unmarshal vlanId 0 as non-nil", func() {
		data := []byte(`{"master": "eth0", "vlanId": 0}`)

		var parsed config.NetConf
		err := json.Unmarshal(data, &parsed)
		Expect(err).NotTo(HaveOccurred())

		Expect(parsed.VlanID).NotTo(BeNil())
		Expect(*parsed.VlanID).To(Equal(0))
	})
})

// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package spclient_test

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spidernet-io/vlan-cni/pkg/spclient"
)

// startUnixServer creates a Unix socket HTTP server for testing
func startUnixServer(socketPath string, handler http.Handler) (func(), error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	return func() {
		server.Close()
		listener.Close()
	}, nil
}

var _ = Describe("Spiderpool Agent Client", func() {
	var (
		socketPath string
		cleanup    func()
		tmpDir     string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "spclient-test-*")
		Expect(err).NotTo(HaveOccurred())
		socketPath = filepath.Join(tmpDir, "test.sock")
	})

	AfterEach(func() {
		if cleanup != nil {
			cleanup()
		}
		os.RemoveAll(tmpDir)
	})

	Context("when agent returns valid response with nic filter", func() {
		BeforeEach(func() {
			var err error
			cleanup, err = startUnixServer(socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal("GET"))
				Expect(r.URL.Query().Get("podName")).To(Equal("web-0"))
				Expect(r.URL.Query().Get("podNamespace")).To(Equal("default"))
				Expect(r.URL.Query().Get("nic")).To(Equal("net1"))

				resp := spclient.WorkloadEndpointResponse{
					IPAssignments: map[string]spclient.IPAssignment{
						"net1": {
							IPs:    []string{"192.168.1.100/24"},
							VlanID: 100,
							MAC:    "aa:bb:cc:dd:ee:ff",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return matching NIC assignment", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("web-0", "default", "net1")
			Expect(err).NotTo(HaveOccurred())
			Expect(assignment).NotTo(BeNil())
			Expect(assignment.VlanID).To(Equal(100))
			Expect(assignment.MAC).To(Equal("aa:bb:cc:dd:ee:ff"))
			Expect(assignment.IPs).To(ConsistOf("192.168.1.100/24"))
		})
	})

	Context("when agent returns multiple NICs without nic filter", func() {
		BeforeEach(func() {
			var err error
			cleanup, err = startUnixServer(socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("nic")).To(BeEmpty())

				resp := spclient.WorkloadEndpointResponse{
					IPAssignments: map[string]spclient.IPAssignment{
						"net1": {
							IPs:    []string{"192.168.1.100/24"},
							VlanID: 100,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return an assignment", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("web-0", "default", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(assignment).NotTo(BeNil())
			Expect(assignment.VlanID).To(Equal(100))
		})
	})

	Context("when requested nic not found in response", func() {
		BeforeEach(func() {
			var err error
			cleanup, err = startUnixServer(socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := spclient.WorkloadEndpointResponse{
					IPAssignments: map[string]spclient.IPAssignment{
						"net2": {
							IPs:    []string{"192.168.2.100/24"},
							VlanID: 200,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return no assignment error", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("web-0", "default", "net1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no assignment for nic"))
			Expect(assignment).To(BeNil())
		})
	})

	Context("when agent returns empty assignments", func() {
		BeforeEach(func() {
			var err error
			cleanup, err = startUnixServer(socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := spclient.WorkloadEndpointResponse{
					IPAssignments: map[string]spclient.IPAssignment{},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return no assignments error", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("web-0", "default", "net1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no IP assignments"))
			Expect(assignment).To(BeNil())
		})
	})

	Context("when agent returns error status", func() {
		BeforeEach(func() {
			var err error
			cleanup, err = startUnixServer(socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error": "Pod not found"}`))
			}))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error with status", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("web-0", "default", "net1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spiderpool-agent returned 404"))
			Expect(assignment).To(BeNil())
		})
	})

	Context("when podName or podNamespace is missing", func() {
		It("should return validation error for missing podName", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("", "default", "net1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("podName and podNamespace are required"))
			Expect(assignment).To(BeNil())
		})

		It("should return validation error for missing podNamespace", func() {
			client := spclient.NewClient(socketPath)
			assignment, err := client.GetWorkloadEndpoint("web-0", "", "net1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("podName and podNamespace are required"))
			Expect(assignment).To(BeNil())
		})
	})

	Context("when agent is unreachable", func() {
		It("should return connection error", func() {
			client := spclient.NewClient("/tmp/nonexistent-socket-12345.sock")
			assignment, err := client.GetWorkloadEndpoint("web-0", "default", "net1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to connect to spiderpool-agent"))
			Expect(assignment).To(BeNil())
		})
	})
})

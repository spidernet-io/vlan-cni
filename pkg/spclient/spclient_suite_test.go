// Copyright 2026 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package spclient_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSpclient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spclient Suite")
}

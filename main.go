// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	log "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins"
	"github.com/ryadavDeqode/nomad-driver-ray/ray"
)

func main() {
	// Serve the plugin
	plugins.Serve(factory)
}

// factory returns a new instance of a nomad driver plugin
func factory(log log.Logger) interface{} {
	return ray.NewPlugin(log)
}

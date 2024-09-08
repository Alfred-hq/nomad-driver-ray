# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

job "nrj1" {
  datacenters = ["dc1"]

  group "ray-remote-task-demo" {
    count = 1
    restart {
      attempts = 0
      mode     = "fail"
    }

    reschedule {
      delay = "5s"
    }

    task "ray-server" {
      driver       = "rayRest"
      kill_timeout = "1m" // increased from default to accomodate ECS.

      config {
        task {
          namespace            = "public91"
          ray_cluster_endpoint = ""
          max_actor_restarts   = "2"
          max_task_retries     = "2"
          actor                = "test_actor"
          runner               = "runner"
        }
      }
    }
  }
}

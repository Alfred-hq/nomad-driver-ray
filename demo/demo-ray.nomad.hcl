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
      kill_timeout = "1m"

      config {
        task {
          namespace            = "public91"
          ray_cluster_endpoint = ""
          max_actor_restarts   = "2"
          max_task_retries     = "2"
          pipeline_file_path   = "/home/deq/AlfredMLWorks/tt.py"
          pipeline_runner      = "print_datetime_forever"
          actor                = "test_actor"
          runner               = "runner"
        }
      }
    }
  }
}

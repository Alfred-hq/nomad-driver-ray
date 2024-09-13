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

    task "v2_trades_workflow_runner" {
      driver       = "rayRest"
      kill_timeout = "1m"

      config {
        task {
          namespace              = "public91"
          ray_cluster_endpoint   = ""
          ray_serve_api_endpoint = ""
          max_actor_restarts     = "2"
          max_task_retries       = "2"
          pipeline_file_path     = "/root/AlfredMLWorks/flyte_projects/gmx_v2/workflows/v2_trades_workflow.py"
          pipeline_runner        = "v2_trades_workflow_runner"
          actor                  = "v2_trades_workflow_runner"
          runner                 = "runner"
        }
      }
    }
  }
}

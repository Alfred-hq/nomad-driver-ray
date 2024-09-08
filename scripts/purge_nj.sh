#!/bin/bash

# Iterate over each line in the job status output
nomad job status | awk 'NR>1 {print $1}' | while read -r job_id; do
  # Stop and purge the job using the job_id
  echo "Stopping and purging job: $job_id"
  nomad job stop -purge "$job_id"
done

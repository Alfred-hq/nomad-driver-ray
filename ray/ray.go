// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ray

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/template"

	"github.com/ryadavDeqode/nomad-driver-ray/templates"
)

// rayRestInterface encapsulates all the required ray rest functionality to
// successfully run tasks via this plugin.
type rayRestInterface interface {

	// DescribeCluster is used to determine the health of the plugin by
	// querying REST server for the cluster and checking its current status. A status
	// other than ACTIVE is considered unhealthy.
	DescribeCluster(ctx context.Context) error

	// RunTask is used to trigger the running of a new RAY REST task based on the
	// provided configuration. Any errors are
	// returned to the caller.
	RunTask(ctx context.Context, cfg TaskConfig) (string, error)

	// // StopTask stops the running ECS task, adding a custom message which can
	// // be viewed via the AWS console specifying it was this Nomad driver which
	// // performed the action.
	// StopTask(ctx context.Context, taskARN string) error
}

type rayRestClient struct {
	rayClusterEndpoint string
}

// DescribeCluster satisfies the DescribeCluster
// interface function.
func (c rayRestClient) DescribeCluster(ctx context.Context) error {
	// Construct the full URL with the IP and port
	url := fmt.Sprintf("%s/api/version", c.rayClusterEndpoint)

	// Make a GET request to the REST API
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to call ray API at %s: %v", url, err)
	}
	defer resp.Body.Close()

	// Check if the HTTP status code is not OK
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ray API request to %s failed with status code: %d", url, resp.StatusCode)
	}

	// If the request is successful and the status code is 200 (OK)
	return nil
}

// RunTask satisfies the ecs.rayRestInterface RunTask interface function.
func (c rayRestClient) RunTask(ctx context.Context, cfg TaskConfig) (string, error) {
    // Parse the template string from the templates package
    tmpl, err := template.New("pythonScript").Parse(templates.DummyTemplate)
    if err != nil {
        return "", fmt.Errorf("failed to parse template: %w", err)
    }

    // Prepare a buffer to hold the generated script
    var pythonScript bytes.Buffer

    // Execute the template with the cfg.Task data
    err = tmpl.Execute(&pythonScript, cfg.Task)
    if err != nil {
        return "", fmt.Errorf("failed to execute template: %w", err)
    }

	// Use triple quotes to pass the script with proper indentation
	scriptContent := pythonScript.String()

	// Triple-quote the entire script content
	entrypoint := fmt.Sprintf(`python3 -c """%s"""`, scriptContent)

    // Build the request payload
    payload := map[string]interface{}{
        "entrypoint":  entrypoint,
        "runtime_env": map[string]interface{}{},
        "job_id":      nil,
        "metadata":    map[string]string{"job_submission_id": "127"},
    }

    // Convert payload to JSON
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return "", fmt.Errorf("failed to marshal payload: %w", err)
    }

    // Create the HTTP request
    url := fmt.Sprintf("%s/api/jobs/", cfg.Task.RayClusterEndpoint)
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
    if err != nil {
        return "", fmt.Errorf("failed to create request: %w", err)
    }

    req.Header.Set("Content-Type", "application/json")

    // Send the request
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return "", fmt.Errorf("failed to send request: %w", err)
    }
    defer resp.Body.Close()

    // Read and process the response
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return "", fmt.Errorf("failed to read response body: %w", err)
    }

    // Check for success
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("request failed with status: %s, response: %s", resp.Status, string(body))
    }

    // Return the actor name (assuming it's still provided in the config)
    return cfg.Task.Actor, nil
}
// // buildTaskInput is used to convert the jobspec supplied configuration input
// // into the appropriate ecs.RunTaskInput object.
// func (c rayRestClient) buildTaskInput(cfg TaskConfig) *ecs.RunTaskInput {
// 	input := ecs.RunTaskInput{
// 		Cluster:              aws.String(c.cluster),
// 		Count:                aws.Int64(1),
// 		StartedBy:            aws.String("nomad-ecs-driver"),
// 		NetworkConfiguration: &ecs.NetworkConfiguration{AwsvpcConfiguration: &ecs.AwsVpcConfiguration{}},
// 	}

// 	if cfg.Task.LaunchType != "" {
// 		if cfg.Task.LaunchType == "EC2" {
// 			input.LaunchType = ecs.LaunchTypeEc2
// 		} else if cfg.Task.LaunchType == "FARGATE" {
// 			input.LaunchType = ecs.LaunchTypeFargate
// 		}
// 	}

// 	if cfg.Task.TaskDefinition != "" {
// 		input.TaskDefinition = aws.String(cfg.Task.TaskDefinition)
// 	}

// 	// Handle the task networking setup.
// 	if cfg.Task.NetworkConfiguration.TaskAWSVPCConfiguration.AssignPublicIP != "" {
// 		assignPublicIp := cfg.Task.NetworkConfiguration.TaskAWSVPCConfiguration.AssignPublicIP
// 		if assignPublicIp == "ENABLED" {
// 			input.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp = ecs.AssignPublicIpEnabled
// 		} else if assignPublicIp == "DISABLED" {
// 			input.NetworkConfiguration.AwsvpcConfiguration.AssignPublicIp = ecs.AssignPublicIpDisabled
// 		}
// 	}
// 	if len(cfg.Task.NetworkConfiguration.TaskAWSVPCConfiguration.SecurityGroups) > 0 {
// 		input.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups = cfg.Task.NetworkConfiguration.TaskAWSVPCConfiguration.SecurityGroups
// 	}
// 	if len(cfg.Task.NetworkConfiguration.TaskAWSVPCConfiguration.Subnets) > 0 {
// 		input.NetworkConfiguration.AwsvpcConfiguration.Subnets = cfg.Task.NetworkConfiguration.TaskAWSVPCConfiguration.Subnets
// 	}

// 	return &input
// }

// func (c rayRestClient) RunTask(ctx context.Context, cfg TaskConfig) (string, error) {
// 	// Build the command to run the Python script with the required arguments
// 	entrypoint := fmt.Sprintf("python3 register_and_start.py %s %s %s", cfg.Task.Namespace, cfg.Task.Actor, cfg.Task.Runner)

// 	// Build the request payload
// 	payload := map[string]interface{}{
// 		"entrypoint":  entrypoint,
// 		"runtime_env": map[string]interface{}{},
// 		"job_id":      nil,
// 		"metadata":    map[string]string{"job_submission_id": "127"},
// 	}

// 	// Convert payload to JSON
// 	payloadBytes, err := json.Marshal(payload)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to marshal payload: %w", err)
// 	}

// 	// Create the HTTP request
// 	url := fmt.Sprintf("%s/api/jobs/", cfg.Task.ClusterEndpoint)
// 	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
// 	if err != nil {
// 		return "", fmt.Errorf("failed to create request: %w", err)
// 	}

// 	req.Header.Set("Content-Type", "application/json")

// 	// Send the request
// 	client := &http.Client{}
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to send request: %w", err)
// 	}
// 	defer resp.Body.Close()

// 	// Read and process the response
// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to read response body: %w", err)
// 	}

// 	// Check for success
// 	if resp.StatusCode != http.StatusOK {
// 		return "", fmt.Errorf("request failed with status: %s, response: %s", resp.Status, string(body))
// 	}

// 	// Return the actor name (assuming it's still provided in the config)
// 	return cfg.Task.Actor, nil
// }

// // StopTask satisfies the ecs.rayRestInterface StopTask interface function.
// func (c rayRestClient) StopTask(ctx context.Context, taskARN string) error {
// 	input := ecs.StopTaskInput{
// 		Cluster: aws.String(c.cluster),
// 		Task:    &taskARN,
// 		Reason:  aws.String("stopped by nomad-ecs-driver automation"),
// 	}

// 	_, err := c.ecsClient.StopTaskRequest(&input).Send(ctx)
// 	return err
// }

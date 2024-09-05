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
	"time"

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
	// // Construct the full URL with the IP and port
	// url := fmt.Sprintf("%s/api/version", c.rayClusterEndpoint)

	// // Make a GET request to the REST API
	// resp, err := http.Get(url)
	// if err != nil {
	// 	return fmt.Errorf("failed to call ray API at %s: %v", url, err)
	// }
	// defer resp.Body.Close()

	// // Check if the HTTP status code is not OK
	// if resp.StatusCode != http.StatusOK {
	// 	return fmt.Errorf("ray API request to %s failed with status code: %d", url, resp.StatusCode)
	// }

	// If the request is successful and the status code is 200 (OK)
	return nil
}

// generateScript generates a Python script from a given template and task configuration.
func generateScript(tmplContent string, task interface{}) (string, error) {
	tmpl, err := template.New("pythonScript").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var script bytes.Buffer
	err = tmpl.Execute(&script, task)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return script.String(), nil
}

// submitJob submits a job to the Ray cluster and handles the HTTP request and response.
func submitJob(ctx context.Context, endpoint string, entrypoint string, jobSubmissionID string) (string, error) {
	// Build the request payload
	payload := map[string]interface{}{
		"entrypoint":  entrypoint,
		"runtime_env": map[string]interface{}{},
		"job_id":      nil,
		"metadata":    map[string]string{"job_submission_id": jobSubmissionID},
	}

	// Convert payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create the HTTP request
	url := fmt.Sprintf("%s/api/jobs/", endpoint)
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

	return string(body), nil
}

func (c rayRestClient) RunTask(ctx context.Context, cfg TaskConfig) (string, error) {
	scriptContent, err := generateScript(templates.DummyTemplate, cfg.Task)
	if err != nil {
		return "", fmt.Errorf("failed to generate script: %w", err)
	}

	entrypoint := fmt.Sprintf(`python3 -c """%s"""`, scriptContent)

	response, err := submitJob(ctx, cfg.Task.RayClusterEndpoint, entrypoint, "127")
	if err != nil {
		return "", err
	}
	fmt.Println("Job submission response:", response)

	data := map[string]interface{}{
		"ServerName": "AlfredRayServeAPI",
	}
	rayServeScript, err := generateScript(templates.RayServeAPITemplate, data)
	if err != nil {
		return "", fmt.Errorf("failed to generate script: %w", err)
	}
	rayServeEntrypoint := fmt.Sprintf(`python3 -c """%s"""`, rayServeScript)
	response, err = submitJob(ctx, cfg.Task.RayClusterEndpoint, rayServeEntrypoint, "128")
	if err != nil {
		fmt.Println("error submitting job:", err)
	}
	fmt.Println("Job submission response:", response)

	// Sleep for 3 seconds before returning
	time.Sleep(5 * time.Second)

	// Process the response if needed, assuming the actor's name is returned
	return cfg.Task.Actor, nil
}

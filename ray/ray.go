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

	RunServeTask(ctx context.Context, cfg TaskConfig) (string, error)

	GetRayServeHealth(ctx context.Context, cfg TaskConfig) (string, error)

	DeleteActor(ctx context.Context, actor_id string) (string, error)


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
	url := fmt.Sprintf("http://localhost:8265/api/version")

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

// GetRayServeHealth sends a GET request to the specified URL
func (c rayRestClient) GetRayServeHealth(ctx context.Context, cfg TaskConfig) (string, error) {
	rayServeEndpoint := cfg.Task.RayServeEndpoint
	url := rayServeEndpoint + "/api/health"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to check ray serve health: %w %s", err, url)
	}
	defer resp.Body.Close()

	// Read the response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var response ActorStatusResponse
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Status != "ok" {
		return "", fmt.Errorf("error from server: %s", response.Error)
	}

	return response.Status, nil
}

// DeleteActor sends a DELETE request to the specified URL
func (c rayRestClient) DeleteActor(ctx context.Context, actor_id string) (string, error) {
	rayServeEndpoint := GlobalConfig.TaskConfig.Task.RayServeEndpoint
	url := rayServeEndpoint + "/api/kill-actor?actor_id=" + actor_id

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create delete request: %w", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to delete actor: %w %s", err, url)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var response ActorStatusResponse
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Status != "success" {
		return "", fmt.Errorf("error from server: %s", response.Error)
	}

	return response.Status, nil
}


func (c rayRestClient) RunTask(ctx context.Context, cfg TaskConfig) (string, error) {
	actorStatus, err := GetActorStatus(context.Background(), cfg.Task.Actor)

	if actorStatus != "ALIVE" || err != nil  {
		scriptContent, err := generateScript(templates.PipelineRunnerTemplate, cfg.Task)
		if err != nil {
			return "", fmt.Errorf("failed to generate script: %w", err)
		}
		
		entrypoint := fmt.Sprintf(`python3 -c """%s"""`, scriptContent)

		_, err = submitJob(ctx, cfg.Task.RayClusterEndpoint, entrypoint, "127")
		if err != nil {
			return "", err
		}
		// Sleep for 10 seconds before returning
		time.Sleep(10 * time.Second)
	} 
	
	// Process the response if needed, assuming the actor's name is returned
	return cfg.Task.Actor, nil
}


func (c rayRestClient) RunServeTask(ctx context.Context, cfg TaskConfig) (string, error) {
	data := map[string]interface{}{
		"ServerName": "AlfredRayServeAPI",
		"Namespace": cfg.Task.Namespace,
	}
	rayServeScript, err := generateScript(templates.RayServeAPITemplate, data)
	if err != nil {
		return "", fmt.Errorf("failed to generate ray serve script: %w", err)
	}
	rayServeEntrypoint := fmt.Sprintf(`python3 -c """%s"""`, rayServeScript)
	_, err = submitJob(ctx, cfg.Task.RayClusterEndpoint, rayServeEntrypoint, "128")
	if err != nil {
		return "", fmt.Errorf("failed to submit ray serve job: %w", err)
	}

	time.Sleep(10 * time.Second)

	return "", nil
}


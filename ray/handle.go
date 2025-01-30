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

	// "net/url"

	"bufio"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/lib/fifo"
	"github.com/hashicorp/nomad/client/stats"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// // These represent the ECS task terminal lifecycle statuses.
// const (
// 	ecsTaskStatusDeactivating   = "DEACTIVATING"
// 	ecsTaskStatusStopping       = "STOPPING"
// 	ecsTaskStatusDeprovisioning = "DEPROVISIONING"
// 	ecsTaskStatusStopped        = "STOPPED"
// )

type taskHandle struct {
	actor            string
	logger           hclog.Logger
	rayRestInterface rayRestInterface

	totalCpuStats  *stats.CpuStats
	userCpuStats   *stats.CpuStats
	systemCpuStats *stats.CpuStats

	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	taskConfig  *drivers.TaskConfig
	procState   drivers.TaskState
	startedAt   time.Time
	completedAt time.Time
	exitResult  *drivers.ExitResult
	doneCh      chan struct{}

	// detach from ecs task instead of killing it if true.
	detach bool

	ctx    context.Context
	cancel context.CancelFunc
}

// ActorLogsRequest represents the JSON payload for requesting actor logs
type ActorLogsRequest struct {
	ActorID string `json:"actor_id"`
}

// ActorLogsResponse represents the JSON response for actor logs
type ActorLogsResponse struct {
	Status string `json:"status"`
	Logs   string `json:"logs,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ActorStatusRequest defines the structure for the request payload
type ActorStatusRequest struct {
	ActorID string `json:"actor_id"`
}

// ActorStatusResponse defines the structure for the response payload
type ActorStatusResponse struct {
	Status      string `json:"status"`
	ActorStatus string `json:"actor_status,omitempty"`
	Error       string `json:"error,omitempty"`
}

// sendRequest sends a POST request with retry logic
func sendRequest(ctx context.Context, url string, payload interface{}, response interface{}) error {
	const retryDelay = 3 * time.Second // Delay between retries

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	client := &http.Client{}

	for attempts := 0; attempts < 3; attempts++ {
		// Create a new POST request
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		// Set the content type to application/json
		req.Header.Set("Content-Type", "application/json")

		// Perform the HTTP request
		resp, err := client.Do(req)
		if err != nil {
			if attempts < 2 {
				time.Sleep(retryDelay) // Wait before retrying
				continue               // Retry
			}
			return fmt.Errorf("failed to send POST request: %w", err)
		}
		defer resp.Body.Close()

		// Read the response body
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			if attempts < 2 {
				time.Sleep(retryDelay) // Wait before retrying
				continue               // Retry
			}
			return fmt.Errorf("failed to read response body: %w", err)
		}

		// Unmarshal the response
		err = json.Unmarshal(responseBody, response)
		if err != nil {
			if attempts < 2 {
				time.Sleep(retryDelay) // Wait before retrying
				continue               // Retry
			}
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		return nil // Success
	}

	return fmt.Errorf("failed after 3 attempts")
}

// GetActorLogs sends a POST request to retrieve logs of a specific actor
func GetActorLogs(ctx context.Context, actorID string) (string, error) {
	rayServeEndpoint := GlobalConfig.TaskConfig.Task.RayServeEndpoint
	url := rayServeEndpoint + "/api/actor-logs"

	payload := ActorLogsRequest{ActorID: actorID}
	var response ActorLogsResponse

	err := sendRequest(ctx, url, payload, &response)
	if err != nil {
		return "", err
	}

	// Check if the response contains an error
	if response.Status != "success" {
		return "", fmt.Errorf("error from server: %s", response.Error)
	}

	return response.Logs, nil // Success, return logs
}

// GetActorStatus sends a POST request to the specified URL with the given actor_id
func GetActorStatus(ctx context.Context, actorID string) (string, error) {
	rayServeEndpoint := GlobalConfig.TaskConfig.Task.RayServeEndpoint
	url := rayServeEndpoint + "/api/actor-status"

	payload := ActorStatusRequest{ActorID: actorID}
	var response ActorStatusResponse

	err := sendRequest(ctx, url, payload, &response)
	if err != nil {
		return "", err
	}

	// Check if the response contains an error
	if response.Status != "success" {
		return "", fmt.Errorf("error from server: %s", response.Error)
	}

	return response.ActorStatus, nil // Success, return actor status
}

func GetActorMemory(ctx context.Context, actorID string) (int, error) {
	url := GlobalConfig.TaskConfig.Task.RayMetricsEndpoint

	// Append ".runner" to the actorID for metric matching
	actorIDWithSuffix := fmt.Sprintf(`%s.runner`, actorID)

	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("error fetching metrics: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var value float64

	// Use regex to match the metric line
	regexPattern := fmt.Sprintf(`ray_component_uss_mb{Component="ray::%s".*} ([0-9.]+)`, regexp.QuoteMeta(actorIDWithSuffix))
	metricRegex := regexp.MustCompile(regexPattern)

	for scanner.Scan() {
		line := scanner.Text()

		// Find matching metric line
		matches := metricRegex.FindStringSubmatch(line)
		if len(matches) == 2 {
			// Parse the matched value
			value, err = strconv.ParseFloat(matches[1], 64)
			if err != nil {
				return 0, fmt.Errorf("error parsing value: %v", err)
			}
			break
		}
	}

	if value == 0 {
		return 0, fmt.Errorf("metric not found or value is zero")
	}

	return int(value), nil
}

func DeleteActor(ctx context.Context, actor_id string) (string, error) {
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

func newTaskHandle(logger hclog.Logger, ts TaskState, taskConfig *drivers.TaskConfig, rayRestInterface rayRestInterface) *taskHandle {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named("handle").With("actor", ts.Actor)

	h := &taskHandle{
		actor:            ts.Actor,
		rayRestInterface: rayRestInterface,
		taskConfig:       taskConfig,
		procState:        drivers.TaskStateRunning,
		startedAt:        ts.StartedAt,
		exitResult:       &drivers.ExitResult{},
		logger:           logger,
		doneCh:           make(chan struct{}),
		detach:           false,
		ctx:              ctx,
		cancel:           cancel,
	}

	return h
}

func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	return &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.procState,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			"actor": h.actor,
		},
	}
}

func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.procState == drivers.TaskStateRunning
}

// runCommand executes a shell command and returns the trimmed stdout or an error
func runCommand(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// GetActorLogs sends a POST request to retrieve logs of a specific actor
func GetActorLogsCLI(ctx context.Context, actorID string) (string, error) {
	rayAddress := GlobalConfig.TaskConfig.Task.RayClusterEndpoint
	command := fmt.Sprintf("ray list actors --address %s --filter 'state=ALIVE' | grep %s", rayAddress, actorID)
	actorDetails, err := runCommand(ctx, command)
	if err != nil {
		return "", fmt.Errorf("failed to fetch actor details: %v", err)
	}

	// Parse the actor ID from the command output
	parts := strings.Fields(actorDetails)
	if len(parts) < 4 {
		return "", fmt.Errorf("unable to parse actor ID from output: %s", actorDetails)
	}
	id := parts[1] // Extract the actor ID (assumes it's the second part)

	// Step 2: Fetch logs for the actor
	// TODO: use varibale
	logsCommand := fmt.Sprintf("ray logs actor --address localhost:6379 --id %s --tail 100", id)
	logs, err := runCommand(ctx, logsCommand)
	if err != nil {
		return "", fmt.Errorf("failed to fetch actor logs: %v", err)
	}

	// Return the logs
	return logs, nil
}

// GetActorStatus sends a POST request to the specified URL with the given actor_id
func GetActorStatusCLI(ctx context.Context, actorID string) (string, error) {
	rayAddress := GlobalConfig.TaskConfig.Task.RayClusterEndpoint
	command := fmt.Sprintf("ray list actors --address %s --filter 'state=ALIVE' | grep %s", rayAddress, actorID)
	actorDetails, err := runCommand(ctx, command)
	if err != nil {
		return "", fmt.Errorf("failed to fetch actor details: %v", err)
	}

	// Parse the actor status from the command output
	parts := strings.Fields(actorDetails)
	if len(parts) < 4 {
		return "", fmt.Errorf("unable to parse actor status from output: %s", actorDetails)
	}
	actorStatus := parts[3] // Extract the actor status (assumes it's the fourth part)

	return actorStatus, nil
}

func (h *taskHandle) run() {
	fmt.Println("Inside Run")
	defer close(h.doneCh)
	h.stateLock.Lock()
	// Open the tasks StdoutPath so we can write task health status updates.
	if h.exitResult == nil {
		h.exitResult = &drivers.ExitResult{}
	}
	h.stateLock.Unlock()

	f, err := fifo.OpenWriter(h.taskConfig.StdoutPath)

	if err != nil {
		h.handleRunError(err, "failed to open task stdout path")
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(f, "failed to close task stdout handle correctly")
			h.logger.Error("failed to close task stdout handle correctly", "error", err)
		}
	}()
	// Set the actor status and logs URLs
	actorID := h.actor
	fmt.Fprintf(f, "Actor - %s \n", actorID)

	// Block until stopped, doing nothing in the meantime.
	for {
		// Call the GetActorStatus function
		actorStatus, err := GetActorStatusCLI(h.ctx, actorID)
		if err != nil {
			fmt.Fprintf(f, "Error retrieving actor status. %v \n", err)
			fmt.Fprintf(f, "Killing exisiting actor.")
			_, err = DeleteActor(context.Background(), actorID)

			if err != nil {
				fmt.Fprintf(f, "Failed to stop remote task [%s] - [%s] \n", actorID, err)
			} else {
				fmt.Fprintf(f, "remote task stopped - [%s]\n", actorID)
			}
			h.handleRunError(err, "Error retrieving actor status.")
			return // TODO: add a retry here
		}

		fmt.Fprintf(f, "Actor is ALIVE, Fetching Memory USAGE \n")

		memory, err := GetActorMemory(h.ctx, actorID)

		if err == nil {
			fmt.Fprintf(f, "Current Memory usage: %d \n", memory)
			memoryThreshold, err := strconv.Atoi(GlobalConfig.TaskConfig.Task.ActorMemoryThreshold)
			if err != nil {
				fmt.Fprintf(f, "error converting ActorMemoryThreshold to int: %v", err)
			} else if memory > memoryThreshold {
				fmt.Fprintf(f, "Memory usage is above threshold of %d. Exiting \n", memoryThreshold)
				_, err = DeleteActor(context.Background(), actorID)
				if err != nil {
					fmt.Fprintf(f, "Failed to stop remote task [%s] - [%s] \n", actorID, err)
				} else {
					fmt.Fprintf(f, "remote task stopped - [%s]\n", actorID)
				}
				h.handleRunError(err, "Memory usage is above threshold.")
				return
			}
		}

		fmt.Fprintf(f, "Actor is Healty, Fetching Logs \n")

		actorLogs, err := GetActorLogsCLI(h.ctx, actorID)

		if err != nil {
			_, err = DeleteActor(context.Background(), actorID)
			if err != nil {
				fmt.Fprintf(f, "Failed to stop remote task [%s] - [%s] \n", actorID, err)
			} else {
				fmt.Fprintf(f, "remote task stopped - [%s]\n", actorID)
			}
			h.handleRunError(err, "Error retrieving actor logs")
			return
		}

		// Sleep for a specified interval before checking again
		select {
		case <-time.After(10 * time.Second):
			// Continue checking after 2 seconds
			now := time.Now().Format(time.RFC3339)
			if _, err := fmt.Fprintf(f, "[%s] - timestamp\n", now); err != nil {
				h.handleRunError(err, "failed to write to stdout")
			}
			if _, err := fmt.Fprintf(f, "[%s] - actorStatus\n", actorStatus); err != nil {
				h.handleRunError(err, "failed to write to stdout")
			}
			if _, err := fmt.Fprintf(f, "%s\n", actorLogs); err != nil {
				h.handleRunError(err, "failed to write to stdout")
			}
		case <-h.ctx.Done():
			// Handle context cancellation
			fmt.Println("Context cancelled. Exiting...")
			return
		}
	}
	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	// Only stop task if we're not detaching.
	if !h.detach {
		if err := h.stopTask(); err != nil {
			h.handleRunError(err, "failed to stop task correctly")
			return
		}
	}

	h.procState = drivers.TaskStateExited
	h.exitResult.ExitCode = 0
	h.exitResult.Signal = 0
	h.completedAt = time.Now()
}

func (h *taskHandle) stop(detach bool) {
	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	// Only allow transitioning from not-detaching to detaching.
	if !h.detach && detach {
		h.detach = detach
	}
	h.cancel()
}

// handleRunError is a convenience function to easily and correctly handle
// terminal errors during the task run lifecycle.
func (h *taskHandle) handleRunError(err error, context string) {
	h.stateLock.Lock()
	h.completedAt = time.Now()
	h.exitResult.ExitCode = 1
	h.exitResult.Err = fmt.Errorf("%s: %v", context, err)
	h.stateLock.Unlock()
}

// stopTask is used to stop the ECS task, and monitor its status until it
// reaches the stopped state.
func (h *taskHandle) stopTask() error {
	return nil
}

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

	// "strings"
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

// GetActorLogs sends a POST request to retrieve logs of a specific actor
func GetActorLogs(ctx context.Context, url, actorID string) (string, error) {
	// Create the request payload
	payload := ActorLogsRequest{ActorID: actorID}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Create a new POST request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set the content type to application/json
	req.Header.Set("Content-Type", "application/json")

	// Perform the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send POST request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Unmarshal the response
	var response ActorLogsResponse
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check if the response contains an error
	if response.Status != "success" {
		return "", fmt.Errorf("error from server: %s", response.Error)
	}

	return response.Logs, nil
}

// GetActorStatus sends a POST request to the specified URL with the given actor_id
func GetActorStatus(ctx context.Context, url, actorID string) (string, error) {
	// Create the request payload
	payload := ActorStatusRequest{ActorID: actorID}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Create a new POST request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set the content type to application/json
	req.Header.Set("Content-Type", "application/json")

	// Perform the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send POST request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Unmarshal the response
	var response ActorStatusResponse
	err = json.Unmarshal(responseBody, &response)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check if the response contains an error
	if response.Status != "success" {
		return "", fmt.Errorf("error from server: %s", response.Error)
	}

	return response.ActorStatus, nil
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

func (h *taskHandle) run() {
	defer close(h.doneCh)
	h.stateLock.Lock()
	if h.exitResult == nil {
		h.exitResult = &drivers.ExitResult{}
	}
	h.stateLock.Unlock()

	// Open the tasks StdoutPath so we can write task health status updates.
	f, err := fifo.OpenWriter(h.taskConfig.StdoutPath)
	if err != nil {
		h.handleRunError(err, "failed to open task stdout path")
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			h.logger.Error("failed to close task stdout handle correctly", "error", err)
		}
	}()

	// Parse the RayServeEndpoint and update the port to 8001 for the Ray Serve endpoint.
	rayServeEndpoint := GlobalConfig.TaskConfig.Task.RayServeEndpoint

	// Set the actor status and logs URLs
	url := rayServeEndpoint + "/api/actor-status"
	logs_url := rayServeEndpoint + "/api/actor-logs"
	actorID := GlobalConfig.TaskConfig.Task.Actor

	// Counter for tracking consecutive not ALIVE statuses.
	notAliveCount := 0

	// Block until stopped, doing nothing in the meantime.
	for {
		fmt.Println(url)
		fmt.Println(actorID)
		// Call the GetActorStatus function
		actorStatus, err := GetActorStatus(h.ctx, url, actorID)
		if err != nil {
			fmt.Println("Error retrieving actor status. Exiting...")
			return
		}

		// Check if the status is still ALIVE
		if actorStatus != "ALIVE" {
			notAliveCount++
			fmt.Printf("Actor status is not ALIVE, count: %d\n", notAliveCount)
			if notAliveCount >= 3 {
				fmt.Println("Actor is no longer ALIVE after 3 attempts. Exiting...")
				break
			}
		} else {
			notAliveCount = 0
		}

		actorLogs, err := GetActorLogs(h.ctx, logs_url, actorID)
		if err != nil {
			fmt.Println("Error retrieving actor logs. Exiting...")
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
			if _, err := fmt.Fprintf(f, "[%s] - actorId\n", logs_url); err != nil {
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

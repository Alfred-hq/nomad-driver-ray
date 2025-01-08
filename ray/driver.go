// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ray

import (
	"context"
	"fmt"
	"strings"
	"time"
	"sync"
	"io"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	pstructs "github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/ryadavDeqode/nomad-driver-ray/version"
	"github.com/hashicorp/nomad/client/lib/fifo"
)

const (
	// pluginName is the name of the plugin.
	pluginName = "rayRest"

	// fingerprintPeriod is the interval at which the driver will send
	// fingerprint responses.
	fingerprintPeriod = 30 * time.Second

	// taskHandleVersion is the version of task handle which this plugin sets
	// and understands how to decode. This is used to allow modification and
	// migration of the task schema used by the plugin.
	taskHandleVersion = 1
)

// TaskState is the state which is encoded in the handle returned in
// StartTask. This information is needed to rebuild the task state and handler
// during recovery.
type TaskState struct {
	TaskConfig    *drivers.TaskConfig
	ContainerName string
	Actor         string
	StartedAt     time.Time
}

type GlobalTaskConfig struct {
	DriverConfig *drivers.TaskConfig
	TaskConfig   TaskConfig
}

var (
	// pluginInfo is the response returned for the PluginInfo RPC.
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDriver,
		PluginApiVersions: []string{drivers.ApiVersion010},
		PluginVersion:     version.Version,
		Name:              pluginName,
	}

	// pluginConfigSpec is the hcl specification returned by the ConfigSchema RPC.
	pluginConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"enabled": hclspec.NewAttr("enabled", "bool", false),
	})

	// taskConfigSpec represents an ECS task configuration object.
	// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/scheduling_tasks.html
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"task": hclspec.NewBlock("task", false, rayRestTaskConfigSpec),
	})

	// awsECSTaskConfigSpec are the high level configuration options for
	// configuring and ECS task.
	rayRestTaskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"namespace":              hclspec.NewAttr("namespace", "string", false),
		"ray_cluster_endpoint":   hclspec.NewAttr("ray_cluster_endpoint", "string", false),
		"ray_serve_api_endpoint": hclspec.NewAttr("ray_serve_api_endpoint", "string", false),
		"max_actor_restarts":     hclspec.NewAttr("max_actor_restarts", "string", false),
		"num_cpus":     		  hclspec.NewAttr("num_cpus", "string", false),
		"max_task_retries":       hclspec.NewAttr("max_task_retries", "string", false),
		"pipeline_file_path":     hclspec.NewAttr("pipeline_file_path", "string", false),
		"pipeline_runner":        hclspec.NewAttr("pipeline_runner", "string", false),
		"actor":                  hclspec.NewAttr("actor", "string", false),
		"runner":                 hclspec.NewAttr("runner", "string", false),
	})

	// // awsECSNetworkConfigSpec is the network configuration for the task.
	// awsECSNetworkConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
	// 	"aws_vpc_configuration": hclspec.NewBlock("aws_vpc_configuration", false, awsECSVPCConfigSpec),
	// })

	// // awsECSVPCConfigSpec is the object representing the networking details
	// // for an ECS task or service.
	// awsECSVPCConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
	// 	"assign_public_ip": hclspec.NewAttr("assign_public_ip", "string", false),
	// 	"security_groups":  hclspec.NewAttr("security_groups", "list(string)", false),
	// 	"subnets":          hclspec.NewAttr("subnets", "list(string)", false),
	// })

	// capabilities is returned by the Capabilities RPC and indicates what
	// optional features this driver supports
	capabilities = &drivers.Capabilities{
		SendSignals: false,
		Exec:        false,
		FSIsolation: drivers.FSIsolationImage,
		RemoteTasks: true,
	}
	GlobalConfig GlobalTaskConfig

	rayServeMutex sync.Mutex
	rayServeCond = sync.NewCond(&rayServeMutex) // Condition variable based on mutex
	isRayServeApiStarted bool
	isRayServeApiRunning bool // To track if it's currently being executed
	
)

// Driver is a driver for running ECS containers
type Driver struct {
	// eventer is used to handle multiplexing of TaskEvents calls such that an
	// event can be broadcast to all callers
	eventer *eventer.Eventer

	// config is the driver configuration set by the SetConfig RPC
	config *DriverConfig

	// nomadConfig is the client config from nomad
	nomadConfig *base.ClientDriverConfig

	// tasks is the in memory datastore mapping taskIDs to rawExecDriverHandles
	tasks *taskStore

	// ctx is the context for the driver. It is passed to other subsystems to
	// coordinate shutdown
	ctx context.Context

	// signalShutdown is called when the driver is shutting down and cancels the
	// ctx passed to any subsystems
	signalShutdown context.CancelFunc

	// logger will log to the Nomad agent
	logger hclog.Logger

	// rayRestInterface is the interface used for communicating with AWS ECS
	client rayRestInterface
}

// DriverConfig is the driver configuration set by the SetConfig RPC call
type DriverConfig struct {
	Enabled            bool   `codec:"enabled"`
	RayClusterEndpoint string `codec:"rayClusterEndpoint"`
}

// TaskConfig is the driver configuration of a task within a job
type TaskConfig struct {
	Task RayTaskConfig `codec:"task"`
}

type RayTaskConfig struct {
	Namespace          string `codec:"namespace"`
	RayClusterEndpoint string `codec:"ray_cluster_endpoint"`
	RayServeEndpoint   string `codec:"ray_serve_api_endpoint"`
	
	MaxActorRestarts   string `codec:"max_actor_restarts"`
	NumCPUs   		   string `codec:"num_cpus"`
	MaxTaskRetries     string `codec:"max_task_retries"`
	PipelineFilePath   string `codec:"pipeline_file_path"`
	PipelineRunner      string `codec:"pipeline_runner"`
	Actor              string `codec:"actor"`
	Runner             string `codec:"runner"`
}

// NewECSDriver returns a new DriverPlugin implementation
func NewPlugin(logger hclog.Logger) drivers.DriverPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named(pluginName)
	return &Driver{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &DriverConfig{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
	}
}

func (d *Driver) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

func (d *Driver) ConfigSchema() (*hclspec.Spec, error) {
	return pluginConfigSpec, nil
}

func (d *Driver) SetConfig(cfg *base.Config) error {
	var config DriverConfig
	if len(cfg.PluginConfig) != 0 {
		if err := base.MsgPackDecode(cfg.PluginConfig, &config); err != nil {
			return err
		}
	}

	d.config = &config
	if cfg.AgentConfig != nil {
		d.nomadConfig = cfg.AgentConfig.Driver
	}

	client, err := d.getRayConfig(config.RayClusterEndpoint)
	if err != nil {
		return fmt.Errorf("failed to get ray client: %v", err)
	}
	d.client = client

	return nil
}

func (d *Driver) getRayConfig(cluster string) (rayRestInterface, error) {
	return rayRestClient{
		rayClusterEndpoint: cluster,
	}, nil
}

func (d *Driver) Shutdown(ctx context.Context) error {
	d.signalShutdown()
	return nil
}

func (d *Driver) TaskConfigSchema() (*hclspec.Spec, error) {
	return taskConfigSpec, nil
}

func (d *Driver) Capabilities() (*drivers.Capabilities, error) {
	return capabilities, nil
}

func (d *Driver) Fingerprint(ctx context.Context) (<-chan *drivers.Fingerprint, error) {
	ch := make(chan *drivers.Fingerprint)
	go d.handleFingerprint(ctx, ch)
	return ch, nil
}

func (d *Driver) handleFingerprint(ctx context.Context, ch chan<- *drivers.Fingerprint) {
	defer close(ch)
	ticker := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			ticker.Reset(fingerprintPeriod)
			ch <- d.buildFingerprint(ctx)
		}
	}
}

func (d *Driver) buildFingerprint(ctx context.Context) *drivers.Fingerprint {
	var health drivers.HealthState
	var desc string
	attrs := map[string]*pstructs.Attribute{}

	if d.config.Enabled {
		if err := d.client.DescribeCluster(ctx); err != nil {
			health = drivers.HealthStateUnhealthy
			desc = err.Error()
			attrs["driver.ecs"] = pstructs.NewBoolAttribute(false)
		} else {
			health = drivers.HealthStateHealthy
			desc = "Healthy"
			attrs["driver.ecs"] = pstructs.NewBoolAttribute(true)
		}
	} else {
		health = drivers.HealthStateUndetected
		desc = "disabled"
	}

	return &drivers.Fingerprint{
		Attributes:        attrs,
		Health:            health,
		HealthDescription: desc,
	}
}

func (d *Driver) RecoverTask(handle *drivers.TaskHandle) error {
	if handle == nil {
		return fmt.Errorf("Handle cannot be nil")
	}

	f, err := fifo.OpenWriter(handle.Config.StdoutPath)

	if err != nil {
		return fmt.Errorf("Failed to open FIFO writer: %v", err)
	}

	fmt.Fprintf(f, "Recovering Ray task - %s\n", handle.Config.ID)

	// If the task is already attached to handle, there's nothing to recover.
	if _, ok := d.tasks.Get(handle.Config.ID); ok {
		fmt.Fprintf(f, "No Ray task to recover; task already exists - %s\n", handle.Config.Name)
		return nil
	}

	// The handle doesn't already exist, try to reattach
	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		fmt.Fprintf(f, "Failed to decode task state from handle - %v\n", err)
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	fmt.Fprintf(f, "Ray task recovered \n", err)

	h := newTaskHandle(d.logger, taskState, handle.Config, d.client)

	if err := d.StartRayServeApi(f); err != nil {
		h.procState = drivers.TaskStateExited
		h.exitResult.ExitCode = 143
		h.exitResult.Signal = 15
		h.completedAt = time.Now()
		fmt.Fprintf(f, "failed to start Ray Serve API: %v\n", err)
		return fmt.Errorf("failed to start Ray Serve API: %v", err)
	}

	var driverConfig TaskConfig
	if err := handle.Config.DecodeDriverConfig(&driverConfig); err != nil {
		fmt.Fprintf(f, "failed to decode driver config:: %v\n", err)
		return fmt.Errorf("failed to decode driver config: %v", err)
	}
	driverConfig.Task.Actor = driverConfig.Task.Actor + "_" + strings.ReplaceAll(handle.Config.AllocID, "-", "")

	_, err = d.client.RunTask(context.Background(), driverConfig)
	if err != nil {
		fmt.Fprintf(f, "failed to start ray task: %v\n", err)
		return fmt.Errorf("failed to start ray task: %v", err)
	}

	d.tasks.Set(handle.Config.ID, h)

	go h.run()
	return nil
}


func (d *Driver) StartRayServeApi(f io.WriteCloser) error {
	rayServeMutex.Lock() // Lock to protect the shared state
    defer rayServeMutex.Unlock()
	
	fmt.Fprintf(f, "Is ray serve started - %t\n", isRayServeApiStarted)

    _, err := d.client.GetRayServeHealth(context.Background())

	if err != nil {
		fmt.Fprintf(f, "Refreshing serve deployment state - %t\n", isRayServeApiStarted)
		isRayServeApiStarted = false
	}

    if isRayServeApiStarted {
        return nil // Ray Serve API already started, no need to run again
    }

	fmt.Fprintf(f, "Is ray serve running -  %t\n", isRayServeApiRunning)

    if isRayServeApiRunning {
        rayServeCond.Wait() // Wait for the first goroutine to finish
        return nil // Once done waiting, just return since it's already started
    }

    // Mark that Ray Serve API is running so others wait
    isRayServeApiRunning = true

    if err != nil {
		fmt.Fprintf(f, "Failed to Get Ray Serve Health - %v\n", err)
        _, err = d.client.RunServeTask(context.Background())
        if err == nil {
            isRayServeApiStarted = true
			fmt.Fprintf(f, "Ray Serve API started successfully\n")
        } else {
			fmt.Fprintf(f, "Failed to start Ray Serve API - %v\n", err)
        }
    } else {
		fmt.Fprintf(f, "Ray Serve API already running \n",)
        isRayServeApiStarted = true
    }

    // Ray Serve API startup has completed, notify other waiting goroutines
    isRayServeApiRunning = false
    rayServeCond.Broadcast() // Wake up all waiting goroutines

    return err
}

func (d *Driver) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	if !d.config.Enabled {
		return nil, nil, fmt.Errorf("disabled")
	}

	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var driverConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&driverConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	d.logger.Info("starting ray remote task", "driver_cfg", hclog.Fmt("%+v", driverConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg
	driverConfig.Task.Actor = driverConfig.Task.Actor + "_" + strings.ReplaceAll(cfg.AllocID, "-", "")

	driverState := TaskState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
		Actor:      driverConfig.Task.Actor,
	}

	d.logger.Info("ray task started", "actor", driverState.Actor, "started_at", driverState.StartedAt)

	h := newTaskHandle(d.logger, driverState, cfg, d.client)

	f, err := fifo.OpenWriter(h.taskConfig.StdoutPath)

	if err != nil {
		return nil, nil, fmt.Errorf("failed to open FIFO writer: %v", err)
	}

	fmt.Fprintf(f, "ray task started\n")
	
	GlobalConfig = GlobalTaskConfig{
		DriverConfig: cfg,
		TaskConfig:   driverConfig,
	}
	
	// Ensure StartRayServeApi is called only once and other tasks wait until it's done
	if err := d.StartRayServeApi(f); err != nil {
		fmt.Fprintf(f, "failed to start Ray Serve API: %v\n", err)
	}

	// Start the task
	_, err = d.client.RunTask(context.Background(), driverConfig)
	if err != nil {
		fmt.Fprintf(f, "failed to start ray task: %v\n", err)
	} 
	// driverState.Actor = actor
	if err := handle.SetDriverState(&driverState); err != nil {
		d.logger.Error("failed to start task, error setting driver state", "error", err)
		h.stop(false)
		fmt.Fprintf(f, "failed to set driver state: %v\n", err)
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	d.tasks.Set(cfg.ID, h)

	go h.run()

	return handle, nil, nil
}

func (d *Driver) WaitTask(ctx context.Context, taskID string) (<-chan *drivers.ExitResult, error) {
	d.logger.Info("WaitTask() called", "task_id", taskID)
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}
	f, err := fifo.OpenWriter(handle.taskConfig.StdoutPath)
	if err != nil {
		fmt.Fprintf(f, "Failed to open writer while stopping \n")
	}
	fmt.Fprintf(f, "inside wait task \n")

	ch := make(chan *drivers.ExitResult)
	go d.handleWait(ctx, handle, ch)

	return ch, nil
}

func (d *Driver) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)

	var result *drivers.ExitResult
	select {
	case <-ctx.Done():
		return
	case <-d.ctx.Done():
		return
	case <-handle.doneCh:
		result = &drivers.ExitResult{
			ExitCode: handle.exitResult.ExitCode,
			Signal:   handle.exitResult.Signal,
			Err:      nil,
		}
	}

	select {
	case <-ctx.Done():
		return
	case <-d.ctx.Done():
		return
	case ch <- result:
	}
}

func getActorId(taskID string) string {
	parts := strings.Split(taskID, "/")
	if len(parts) != 3 {
		return "Invalid input format"
	}
	alloc := parts[0]
	workflow := parts[1]

	return fmt.Sprintf("%s_%s", workflow, strings.ReplaceAll(alloc, "-", ""))
}

func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	d.logger.Info("stopping remote task", "task_id", taskID, "timeout", timeout, "signal", signal)
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}
	f, err := fifo.OpenWriter(handle.taskConfig.StdoutPath)

	if err != nil {
		fmt.Fprintf(f, "Failed to open writer while stopping \n")
	}
	actorId := getActorId(taskID)
	_, err = d.client.DeleteActor(context.Background(), actorId)


	if err != nil {
		fmt.Fprintf(f, "Failed to stop remote task [%s] - [%s] \n", actorId, err)
	} else {
		fmt.Fprintf(f, "remote task stopped - [%s]\n", actorId)
	}


	// Detach if that's the signal, otherwise proceed to terminate
	detach := signal == drivers.DetachSignal
	handle.stop(detach)

	// Wait for the task handle to signal completion
	select {
	case <-handle.doneCh:
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for remote task (id=%s) to stop (detach=%t)",
			taskID, detach)
	}

	d.logger.Info("remote task stopped", "task_id", taskID, "timeout", timeout, "signal", signal)
	return nil
}

func (d *Driver) DestroyTask(taskID string, force bool) error {
	d.logger.Info("destroying ray task", "task_id", taskID, "force", force)
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	f, err := fifo.OpenWriter(handle.taskConfig.StdoutPath)
	if err != nil {
		fmt.Fprintf(f, "Failed to open writer while stopping \n")
	}

	fmt.Fprintf(f, "running destroy task \n")

	if handle.IsRunning() && !force {
		fmt.Fprintf(f, "cannot destroy running task \n")
		return fmt.Errorf("cannot destroy running task")
	}

	// Safe to always kill here as detaching will have already happened
	handle.stop(false)

	d.tasks.Delete(taskID)
	fmt.Fprintf(f, "destoryed task_id [%s] force [%s] \n", taskID, force)
	d.logger.Info("ray task destroyed", "task_id", taskID, "force", force)
	return nil
}

func (d *Driver) InspectTask(taskID string) (*drivers.TaskStatus, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}
	return handle.TaskStatus(), nil
}

func (d *Driver) TaskStats(ctx context.Context, taskID string, interval time.Duration) (<-chan *structs.TaskResourceUsage, error) {
	d.logger.Info("sending ray task stats", "task_id", taskID)
	_, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	ch := make(chan *drivers.TaskResourceUsage)

	go func() {
		defer d.logger.Info("stopped sending ray task stats", "task_id", taskID)
		defer close(ch)
		for {
			select {
			case <-time.After(interval):

				// Nomad core does not currently have any resource based
				// support for remote drivers. Once this changes, we may be
				// able to report actual usage here.
				//
				// This is required, otherwise the driver panics.
				ch <- &structs.TaskResourceUsage{
					ResourceUsage: &drivers.ResourceUsage{
						MemoryStats: &drivers.MemoryStats{},
						CpuStats:    &drivers.CpuStats{},
					},
					Timestamp: time.Now().UTC().UnixNano(),
				}
			case <-ctx.Done():
				return
			}

		}
	}()

	return ch, nil
}

func (d *Driver) TaskEvents(ctx context.Context) (<-chan *drivers.TaskEvent, error) {
	d.logger.Info("retrieving task events")
	return d.eventer.TaskEvents(ctx)
}

func (d *Driver) SignalTask(_ string, _ string) error {
	return fmt.Errorf("ray rest driver does not support signals")
}

func (d *Driver) ExecTask(_ string, _ []string, _ time.Duration) (*drivers.ExecTaskResult, error) {
	return nil, fmt.Errorf("ray rest driver does not support exec")
}

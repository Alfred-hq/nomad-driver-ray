// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ray

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	pstructs "github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/ryadavDeqode/nomad-driver-ray/version"
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
		"enabled":            hclspec.NewAttr("enabled", "bool", false),
		"rayClusterEndpoint": hclspec.NewAttr("rayClusterEndpoint", "string", false),
	})

	// taskConfigSpec represents an ECS task configuration object.
	// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/scheduling_tasks.html
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"task": hclspec.NewBlock("task", false, rayRestTaskConfigSpec),
	})

	// awsECSTaskConfigSpec are the high level configuration options for
	// configuring and ECS task.
	rayRestTaskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"namespace":            hclspec.NewAttr("namespace", "string", false),
		"ray_cluster_endpoint": hclspec.NewAttr("ray_cluster_endpoint", "string", false),
		"ray_serve_endpoint":   hclspec.NewAttr("ray_serve_endpoint", "string", false),
		"actor":                hclspec.NewAttr("actor", "string", false),
		"runner":               hclspec.NewAttr("runner", "string", false),
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
	RayServeEndpoint   string `codec:"ray_serve_endpoint"`
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
	d.logger.Info("recovering Ray task", "version", handle.Version,
		"task_config.id", handle.Config.ID, "task_state", handle.State,
		"driver_state_bytes", len(handle.DriverState))
	if handle == nil {
		return fmt.Errorf("handle cannot be nil")
	}

	// If the task is already attached to handle, there's nothing to recover.
	if _, ok := d.tasks.Get(handle.Config.ID); ok {
		d.logger.Info("no Ray task to recover; task already exists",
			"task_id", handle.Config.ID,
			"task_name", handle.Config.Name,
		)
		return nil
	}

	// The handle doesn't already exist, try to reattach
	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		d.logger.Error("failed to decode task state from handle", "error", err, "task_id", handle.Config.ID)
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	d.logger.Info("Ray task recovered", "actor", taskState.Actor,
		"started_at", taskState.StartedAt)

	h := newTaskHandle(d.logger, taskState, handle.Config, d.client)

	d.tasks.Set(handle.Config.ID, h)

	go h.run()
	return nil
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

	actor, err := d.client.RunTask(context.Background(), driverConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start ray task: %v", err)
	}

	driverState := TaskState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
		Actor:      actor,
	}

	d.logger.Info("ray task started", "actor", driverState.Actor, "started_at", driverState.StartedAt)

	h := newTaskHandle(d.logger, driverState, cfg, d.client)

	if err := handle.SetDriverState(&driverState); err != nil {
		d.logger.Error("failed to start task, error setting driver state", "error", err)
		h.stop(false)
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	d.tasks.Set(cfg.ID, h)
	GlobalConfig = GlobalTaskConfig{
		DriverConfig: cfg,
		TaskConfig:   driverConfig,
	}
	go h.run()
	return handle, nil, nil
}

func (d *Driver) WaitTask(ctx context.Context, taskID string) (<-chan *drivers.ExitResult, error) {
	d.logger.Info("WaitTask() called", "task_id", taskID)
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

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

func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	d.logger.Info("stopping remote task", "task_id", taskID, "timeout", timeout, "signal", signal)
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
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

	if handle.IsRunning() && !force {
		return fmt.Errorf("cannot destroy running task")
	}

	// Safe to always kill here as detaching will have already happened
	handle.stop(false)

	d.tasks.Delete(taskID)
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

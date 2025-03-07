package mock

import (
	"fmt"
	"time"

	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
)

func Job() *structs.Job {
	job := &structs.Job{
		Region:      "global",
		ID:          fmt.Sprintf("mock-service-%s", uuid.Generate()),
		Name:        "my-job",
		Namespace:   structs.DefaultNamespace,
		Type:        structs.JobTypeService,
		Priority:    50,
		AllAtOnce:   false,
		Datacenters: []string{"dc1"},
		Constraints: []*structs.Constraint{
			{
				LTarget: "${attr.kernel.name}",
				RTarget: "linux",
				Operand: "=",
			},
		},
		TaskGroups: []*structs.TaskGroup{
			{
				Name:  "web",
				Count: 10,
				Constraints: []*structs.Constraint{
					{
						LTarget: "${attr.consul.version}",
						RTarget: ">= 1.7.0",
						Operand: structs.ConstraintSemver,
					},
				},
				EphemeralDisk: &structs.EphemeralDisk{
					SizeMB: 150,
				},
				RestartPolicy: &structs.RestartPolicy{
					Attempts: 3,
					Interval: 10 * time.Minute,
					Delay:    1 * time.Minute,
					Mode:     structs.RestartPolicyModeDelay,
				},
				ReschedulePolicy: &structs.ReschedulePolicy{
					Attempts:      2,
					Interval:      10 * time.Minute,
					Delay:         5 * time.Second,
					DelayFunction: "constant",
				},
				Migrate: structs.DefaultMigrateStrategy(),
				Networks: []*structs.NetworkResource{
					{
						Mode: "host",
						DynamicPorts: []structs.Port{
							{Label: "http"},
							{Label: "admin"},
						},
					},
				},
				Tasks: []*structs.Task{
					{
						Name:   "web",
						Driver: "exec",
						Config: map[string]interface{}{
							"command": "/bin/date",
						},
						Env: map[string]string{
							"FOO": "bar",
						},
						Services: []*structs.Service{
							{
								Name:      "${TASK}-frontend",
								PortLabel: "http",
								Tags:      []string{"pci:${meta.pci-dss}", "datacenter:${node.datacenter}"},
								Checks: []*structs.ServiceCheck{
									{
										Name:     "check-table",
										Type:     structs.ServiceCheckScript,
										Command:  "/usr/local/check-table-${meta.database}",
										Args:     []string{"${meta.version}"},
										Interval: 30 * time.Second,
										Timeout:  5 * time.Second,
									},
								},
							},
							{
								Name:      "${TASK}-admin",
								PortLabel: "admin",
							},
						},
						LogConfig: structs.DefaultLogConfig(),
						Resources: &structs.Resources{
							CPU:      500,
							MemoryMB: 256,
						},
						Meta: map[string]string{
							"foo": "bar",
						},
					},
				},
				Meta: map[string]string{
					"elb_check_type":     "http",
					"elb_check_interval": "30s",
					"elb_check_min":      "3",
				},
			},
		},
		Meta: map[string]string{
			"owner": "armon",
		},
		Status:         structs.JobStatusPending,
		Version:        0,
		CreateIndex:    42,
		ModifyIndex:    99,
		JobModifyIndex: 99,
	}
	job.Canonicalize()
	return job
}

func JobWithScalingPolicy() (*structs.Job, *structs.ScalingPolicy) {
	job := Job()
	policy := &structs.ScalingPolicy{
		ID:      uuid.Generate(),
		Min:     int64(job.TaskGroups[0].Count),
		Max:     int64(job.TaskGroups[0].Count),
		Type:    structs.ScalingPolicyTypeHorizontal,
		Policy:  map[string]interface{}{},
		Enabled: true,
	}
	policy.TargetTaskGroup(job, job.TaskGroups[0])
	job.TaskGroups[0].Scaling = policy
	return job, policy
}

func MultiTaskGroupJob() *structs.Job {
	job := Job()
	apiTaskGroup := &structs.TaskGroup{
		Name:  "api",
		Count: 10,
		EphemeralDisk: &structs.EphemeralDisk{
			SizeMB: 150,
		},
		RestartPolicy: &structs.RestartPolicy{
			Attempts: 3,
			Interval: 10 * time.Minute,
			Delay:    1 * time.Minute,
			Mode:     structs.RestartPolicyModeDelay,
		},
		ReschedulePolicy: &structs.ReschedulePolicy{
			Attempts:      2,
			Interval:      10 * time.Minute,
			Delay:         5 * time.Second,
			DelayFunction: "constant",
		},
		Migrate: structs.DefaultMigrateStrategy(),
		Networks: []*structs.NetworkResource{
			{
				Mode: "host",
				DynamicPorts: []structs.Port{
					{Label: "http"},
					{Label: "admin"},
				},
			},
		},
		Tasks: []*structs.Task{
			{
				Name:   "api",
				Driver: "exec",
				Config: map[string]interface{}{
					"command": "/bin/date",
				},
				Env: map[string]string{
					"FOO": "bar",
				},
				Services: []*structs.Service{
					{
						Name:      "${TASK}-backend",
						PortLabel: "http",
						Tags:      []string{"pci:${meta.pci-dss}", "datacenter:${node.datacenter}"},
						Checks: []*structs.ServiceCheck{
							{
								Name:     "check-table",
								Type:     structs.ServiceCheckScript,
								Command:  "/usr/local/check-table-${meta.database}",
								Args:     []string{"${meta.version}"},
								Interval: 30 * time.Second,
								Timeout:  5 * time.Second,
							},
						},
					},
					{
						Name:      "${TASK}-admin",
						PortLabel: "admin",
					},
				},
				LogConfig: structs.DefaultLogConfig(),
				Resources: &structs.Resources{
					CPU:      500,
					MemoryMB: 256,
				},
				Meta: map[string]string{
					"foo": "bar",
				},
			},
		},
		Meta: map[string]string{
			"elb_check_type":     "http",
			"elb_check_interval": "30s",
			"elb_check_min":      "3",
		},
	}
	job.TaskGroups = append(job.TaskGroups, apiTaskGroup)
	job.Canonicalize()
	return job
}

func SystemBatchJob() *structs.Job {
	job := &structs.Job{
		Region:      "global",
		ID:          fmt.Sprintf("mock-sysbatch-%s", uuid.Short()),
		Name:        "my-sysbatch",
		Namespace:   structs.DefaultNamespace,
		Type:        structs.JobTypeSysBatch,
		Priority:    10,
		Datacenters: []string{"dc1"},
		Constraints: []*structs.Constraint{
			{
				LTarget: "${attr.kernel.name}",
				RTarget: "linux",
				Operand: "=",
			},
		},
		TaskGroups: []*structs.TaskGroup{{
			Count: 1,
			Name:  "pinger",
			Tasks: []*structs.Task{{
				Name:   "ping-example",
				Driver: "exec",
				Config: map[string]interface{}{
					"command": "/usr/bin/ping",
					"args":    []string{"-c", "5", "example.com"},
				},
				LogConfig: structs.DefaultLogConfig(),
			}},
		}},

		Status:         structs.JobStatusPending,
		Version:        0,
		CreateIndex:    42,
		ModifyIndex:    99,
		JobModifyIndex: 99,
	}
	job.Canonicalize()
	return job
}

func MultiregionJob() *structs.Job {
	job := Job()
	update := *structs.DefaultUpdateStrategy
	job.Update = update
	job.TaskGroups[0].Update = &update
	job.Multiregion = &structs.Multiregion{
		Strategy: &structs.MultiregionStrategy{
			MaxParallel: 1,
			OnFailure:   "fail_all",
		},
		Regions: []*structs.MultiregionRegion{
			{
				Name:        "west",
				Count:       2,
				Datacenters: []string{"west-1", "west-2"},
				Meta:        map[string]string{"region_code": "W"},
			},
			{
				Name:        "east",
				Count:       1,
				Datacenters: []string{"east-1"},
				Meta:        map[string]string{"region_code": "E"},
			},
		},
	}
	return job
}

func BatchJob() *structs.Job {
	job := &structs.Job{
		Region:      "global",
		ID:          fmt.Sprintf("mock-batch-%s", uuid.Generate()),
		Name:        "batch-job",
		Namespace:   structs.DefaultNamespace,
		Type:        structs.JobTypeBatch,
		Priority:    50,
		AllAtOnce:   false,
		Datacenters: []string{"dc1"},
		TaskGroups: []*structs.TaskGroup{
			{
				Name:  "web",
				Count: 10,
				EphemeralDisk: &structs.EphemeralDisk{
					SizeMB: 150,
				},
				RestartPolicy: &structs.RestartPolicy{
					Attempts: 3,
					Interval: 10 * time.Minute,
					Delay:    1 * time.Minute,
					Mode:     structs.RestartPolicyModeDelay,
				},
				ReschedulePolicy: &structs.ReschedulePolicy{
					Attempts:      2,
					Interval:      10 * time.Minute,
					Delay:         5 * time.Second,
					DelayFunction: "constant",
				},
				Tasks: []*structs.Task{
					{
						Name:   "web",
						Driver: "mock_driver",
						Config: map[string]interface{}{
							"run_for": "500ms",
						},
						Env: map[string]string{
							"FOO": "bar",
						},
						LogConfig: structs.DefaultLogConfig(),
						Resources: &structs.Resources{
							CPU:      100,
							MemoryMB: 100,
							Networks: []*structs.NetworkResource{
								{
									MBits: 50,
								},
							},
						},
						Meta: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
		},
		Status:         structs.JobStatusPending,
		Version:        0,
		CreateIndex:    43,
		ModifyIndex:    99,
		JobModifyIndex: 99,
	}
	job.Canonicalize()
	return job
}

func SystemJob() *structs.Job {
	job := &structs.Job{
		Region:      "global",
		Namespace:   structs.DefaultNamespace,
		ID:          fmt.Sprintf("mock-system-%s", uuid.Generate()),
		Name:        "my-job",
		Type:        structs.JobTypeSystem,
		Priority:    100,
		AllAtOnce:   false,
		Datacenters: []string{"dc1"},
		Constraints: []*structs.Constraint{
			{
				LTarget: "${attr.kernel.name}",
				RTarget: "linux",
				Operand: "=",
			},
		},
		TaskGroups: []*structs.TaskGroup{
			{
				Name:  "web",
				Count: 1,
				RestartPolicy: &structs.RestartPolicy{
					Attempts: 3,
					Interval: 10 * time.Minute,
					Delay:    1 * time.Minute,
					Mode:     structs.RestartPolicyModeDelay,
				},
				EphemeralDisk: structs.DefaultEphemeralDisk(),
				Tasks: []*structs.Task{
					{
						Name:   "web",
						Driver: "exec",
						Config: map[string]interface{}{
							"command": "/bin/date",
						},
						Env: map[string]string{},
						Resources: &structs.Resources{
							CPU:      500,
							MemoryMB: 256,
							Networks: []*structs.NetworkResource{
								{
									MBits:        50,
									DynamicPorts: []structs.Port{{Label: "http"}},
								},
							},
						},
						LogConfig: structs.DefaultLogConfig(),
					},
				},
			},
		},
		Meta: map[string]string{
			"owner": "armon",
		},
		Status:      structs.JobStatusPending,
		CreateIndex: 42,
		ModifyIndex: 99,
	}
	job.Canonicalize()
	return job
}

func PeriodicJob() *structs.Job {
	job := Job()
	job.Type = structs.JobTypeBatch
	job.Periodic = &structs.PeriodicConfig{
		Enabled:  true,
		SpecType: structs.PeriodicSpecCron,
		Spec:     "*/30 * * * *",
	}
	job.Status = structs.JobStatusRunning
	job.TaskGroups[0].Migrate = nil
	return job
}

func MaxParallelJob() *structs.Job {
	update := *structs.DefaultUpdateStrategy
	update.MaxParallel = 0
	job := &structs.Job{
		Region:      "global",
		ID:          fmt.Sprintf("mock-service-%s", uuid.Generate()),
		Name:        "my-job",
		Namespace:   structs.DefaultNamespace,
		Type:        structs.JobTypeService,
		Priority:    50,
		AllAtOnce:   false,
		Datacenters: []string{"dc1"},
		Constraints: []*structs.Constraint{
			{
				LTarget: "${attr.kernel.name}",
				RTarget: "linux",
				Operand: "=",
			},
		},
		Update: update,
		TaskGroups: []*structs.TaskGroup{
			{
				Name:  "web",
				Count: 10,
				EphemeralDisk: &structs.EphemeralDisk{
					SizeMB: 150,
				},
				RestartPolicy: &structs.RestartPolicy{
					Attempts: 3,
					Interval: 10 * time.Minute,
					Delay:    1 * time.Minute,
					Mode:     structs.RestartPolicyModeDelay,
				},
				ReschedulePolicy: &structs.ReschedulePolicy{
					Attempts:      2,
					Interval:      10 * time.Minute,
					Delay:         5 * time.Second,
					DelayFunction: "constant",
				},
				Migrate: structs.DefaultMigrateStrategy(),
				Update:  &update,
				Tasks: []*structs.Task{
					{
						Name:   "web",
						Driver: "exec",
						Config: map[string]interface{}{
							"command": "/bin/date",
						},
						Env: map[string]string{
							"FOO": "bar",
						},
						Services: []*structs.Service{
							{
								Name:      "${TASK}-frontend",
								PortLabel: "http",
								Tags:      []string{"pci:${meta.pci-dss}", "datacenter:${node.datacenter}"},
								Checks: []*structs.ServiceCheck{
									{
										Name:     "check-table",
										Type:     structs.ServiceCheckScript,
										Command:  "/usr/local/check-table-${meta.database}",
										Args:     []string{"${meta.version}"},
										Interval: 30 * time.Second,
										Timeout:  5 * time.Second,
									},
								},
							},
							{
								Name:      "${TASK}-admin",
								PortLabel: "admin",
							},
						},
						LogConfig: structs.DefaultLogConfig(),
						Resources: &structs.Resources{
							CPU:      500,
							MemoryMB: 256,
							Networks: []*structs.NetworkResource{
								{
									MBits: 50,
									DynamicPorts: []structs.Port{
										{Label: "http"},
										{Label: "admin"},
									},
								},
							},
						},
						Meta: map[string]string{
							"foo": "bar",
						},
					},
				},
				Meta: map[string]string{
					"elb_check_type":     "http",
					"elb_check_interval": "30s",
					"elb_check_min":      "3",
				},
			},
		},
		Meta: map[string]string{
			"owner": "armon",
		},
		Status:         structs.JobStatusPending,
		Version:        0,
		CreateIndex:    42,
		ModifyIndex:    99,
		JobModifyIndex: 99,
	}
	job.Canonicalize()
	return job
}

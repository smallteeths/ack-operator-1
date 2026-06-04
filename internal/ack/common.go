package ack

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	"github.com/alibabacloud-go/tea/tea"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	DefaultNodePoolName = "default-nodepool"
)

// Status indicates how to handle the respDefaultNodePoolNameonse from a request to update a resource
type Status int

// Status indicators
const (
	// Changed means the request to change resource was accepted and change is in progress
	Changed Status = iota
	// Retry means the request to change resource was rejected due to an expected error and should be retried later
	Retry
	// NotChanged means the resource was not changed, either due to error or because it was unnecessary
	NotChanged
)

// State of instance
const (
	ClusterStatusRunning  = "running"
	ClusterStatusError    = "failed"
	ClusterStatusUpdating = "updating"
	ClusterStatusScaling  = "scaling"
	ClusterStatusRemoving = "removing"
)

// State of node pool
const (
	NodePoolStatusActive   = "active"
	NodePoolStatusInitial  = "initial"
	NodePoolStatusScaling  = "scaling"
	NodePoolStatusRemoving = "removing"
	NodePoolStatusDeleting = "deleting"
	NodePoolStatusUpdating = "updating"
)

const (
	UpdateK8sRunningStatus   = "running"
	UpdateK8sPauseStatus     = "pause"
	UpdateK8sFailStatus      = "fail"
	UpdateK8sSuccessStatus   = "success"
	UpdateK8SError           = "Upgrade k8s version error"
	UpdateK8SVersionApiError = "Please check that the version of k8s to be upgraded is entered correctly"
)

const (
	waitSec      = 30
	backoffSteps = 12
)

var backoff = wait.Backoff{
	Duration: waitSec * time.Second,
	Steps:    backoffSteps,
}

func ConvertAddons(configSpec *ackv1.ACKClusterConfigSpec) []*ackapi.Addon {
	if configSpec == nil || len(configSpec.Addons) == 0 {
		// flannel
		return nil
	}

	addons := make([]*ackapi.Addon, len(configSpec.Addons))
	for i, addon := range configSpec.Addons {
		name := addon.Name
		config := addon.Config
		addons[i] = &ackapi.Addon{
			Name:   &name,
			Config: &config,
		}
	}
	return addons
}

func SplitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			res = append(res, p)
		}
	}
	return res
}

func StringPtrSliceToStringSliceRaw(src []*string) []string {
	dst := make([]string, len(src))
	for i, p := range src {
		if p != nil {
			dst[i] = *p
		}
	}
	return dst
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "ErrorClusterNotFound") ||
		strings.Contains(errMsg, "ACK configSpec is nil") ||
		strings.Contains(errMsg, "clusterID is empty")
}

// validateCreateRequest checks a config for the ability to generate a create request
func validateCreateRequest(configSpec *ackv1.ACKClusterConfigSpec) error {
	if configSpec.Name == "" {
		return fmt.Errorf("cluster display name is required")
	} else if configSpec.RegionID == "" {
		return fmt.Errorf("region id is required")
	}
	if len(configSpec.ZoneIDs) == 0 {
		if configSpec.VpcID == "" {
			return fmt.Errorf("vpcId is required if zoneIds are not provided")
		}
	} else if configSpec.VpcID != "" || len(configSpec.VswitchIds) != 0 {
		return fmt.Errorf("zoneIds should not be used together with vpcId and vSwitchIds")
	}
	return nil
}

// newClusterCreateRequest creates a CreateClusterRequest that can be submitted to ACK
func newClusterCreateRequest(configSpec *ackv1.ACKClusterConfigSpec) *ackapi.CreateClusterRequest {
	req := &ackapi.CreateClusterRequest{}

	req.Name = tea.String(configSpec.Name)
	req.ClusterType = tea.String(configSpec.ClusterType)
	req.ClusterSpec = tea.String(configSpec.ClusterSpec)
	req.RegionId = tea.String(configSpec.RegionID)
	req.KubernetesVersion = tea.String(configSpec.KubernetesVersion)
	req.Vpcid = tea.String(configSpec.VpcID)
	req.ContainerCidr = tea.String(configSpec.ContainerCidr)
	req.ServiceCidr = tea.String(configSpec.ServiceCidr)
	req.NodeCidrMask = tea.String(strconv.Itoa(int(configSpec.NodeCidrMask)))
	req.SnatEntry = tea.Bool(configSpec.SnatEntry)
	req.ProxyMode = tea.String(configSpec.ProxyMode)
	req.EndpointPublicAccess = tea.Bool(configSpec.EndpointPublicAccess)
	req.SecurityGroupId = tea.String(configSpec.SecurityGroupID)
	req.SshFlags = tea.Bool(configSpec.SSHFlags)
	req.Addons = ConvertAddons(configSpec)
	req.VswitchIds = tea.StringSlice(configSpec.VswitchIds)
	req.ZoneIds = tea.StringSlice(configSpec.ZoneIDs)
	// PodVswitchIds 虽然标记了废弃，但是目前还是需要传入
	req.PodVswitchIds = tea.StringSlice(configSpec.PodVswitchIds)
	req.DeletionProtection = tea.Bool(configSpec.DeletionProtection)

	// get worker creation info from default node pool
	getInitWorkerFromDefaultNodePool(configSpec, req)

	return req
}

func getInitWorkerFromDefaultNodePool(configSpec *ackv1.ACKClusterConfigSpec, req *ackapi.CreateClusterRequest) {
	nodePools := make([]*ackapi.Nodepool, 0, len(configSpec.NodePoolList))

	for _, pool := range configSpec.NodePoolList {
		var dataDiskList []*ackapi.DataDisk
		for _, dataDisk := range pool.DataDisk {
			dataDiskList = append(dataDiskList, &ackapi.DataDisk{
				Category:             tea.String(dataDisk.Category),
				Size:                 tea.Int64(dataDisk.Size),
				Encrypted:            tea.String(dataDisk.Encrypted),
				AutoSnapshotPolicyId: tea.String(dataDisk.AutoSnapshotPolicyID),
			})
		}

		enable := false
		minIns := pool.InstancesNum
		maxIns := pool.InstancesNum

		if pool.AutoScalingEnabled != nil && *pool.AutoScalingEnabled {
			enable = true
			if pool.MinInstances != nil {
				minIns = *pool.MinInstances
			}
			if pool.MaxInstances != nil {
				maxIns = *pool.MaxInstances
			}
		}

		if enable && minIns > maxIns {
			minIns, maxIns = maxIns, minIns
		}

		scalingGroup := &ackapi.NodepoolScalingGroup{
			AutoRenew:          tea.Bool(pool.AutoRenew),
			AutoRenewPeriod:    tea.Int64(pool.AutoRenewPeriod),
			InstanceChargeType: tea.String(pool.InstanceChargeType),
			InstanceTypes:      tea.StringSlice(pool.InstanceTypes),
			KeyPair:            tea.String(pool.KeyPair),
			Period:             tea.Int64(pool.Period),
			PeriodUnit:         tea.String(pool.PeriodUnit),
			ImageType:          tea.String(pool.Platform),
			DataDisks:          dataDiskList,
			SystemDiskCategory: tea.String(pool.SystemDiskCategory),
			SystemDiskSize:     tea.Int64(pool.SystemDiskSize),
			VswitchIds:         tea.StringSlice(pool.VSwitchIds),
		}

		if !enable {
			scalingGroup.DesiredSize = tea.Int64(pool.InstancesNum)
		}

		nodePools = append(nodePools, &ackapi.Nodepool{
			AutoScaling: &ackapi.NodepoolAutoScaling{
				Enable:       tea.Bool(enable),
				MaxInstances: tea.Int64(maxIns),
				MinInstances: tea.Int64(minIns),
				Type:         tea.String(pool.ScalingType),
			},
			NodepoolInfo: &ackapi.NodepoolNodepoolInfo{
				Name: tea.String(pool.Name),
			},
			KubernetesConfig: &ackapi.NodepoolKubernetesConfig{
				Runtime:        tea.String(pool.Runtime),
				RuntimeVersion: tea.String(pool.RuntimeVersion),
			},
			ScalingGroup: scalingGroup,
		})
	}

	req.Nodepools = nodePools
}

func cleanStringSlice(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}

	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	return out
}

func cleanTeaStringSlice(in []*string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}

	for _, v := range in {
		if v == nil {
			continue
		}
		s := tea.StringValue(v)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	return out
}

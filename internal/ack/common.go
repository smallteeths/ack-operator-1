package ack

import (
	"fmt"
	"github.com/alibabacloud-go/tea/tea"
	"strconv"
	"strings"
	"time"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
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
	if strings.Contains(err.Error(), "ErrorClusterNotFound") {
		return true
	}
	return false
}

// validateCreateRequest checks a config for the ability to generate a create request
func validateCreateRequest(configSpec *ackv1.ACKClusterConfigSpec) error {
	if configSpec.Name == "" {
		return fmt.Errorf("cluster display name is required")
	} else if configSpec.RegionID == "" {
		return fmt.Errorf("region id is required")
	} else if configSpec.VpcID == "" && !configSpec.SnatEntry {
		return fmt.Errorf("snat entry is required when vpc is auto created")
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
	// 在 Terway 网络模式下，Pod 网络的 IP 地址分配方式已优化，不再需要单独指定 pod_vswitch_ids，而是直接使用 vswitch_ids
	//req.PodVswitchIds = tea.StringSlice(configSpec.PodVswitchIds)

	// get worker creation info from default node pool
	getInitWorkerFromDefaultNodePool(configSpec, req)

	return req
}

func getInitWorkerFromDefaultNodePool(configSpec *ackv1.ACKClusterConfigSpec, req *ackapi.CreateClusterRequest) {
	nodePools := make([]*ackapi.Nodepool, 0, 1)
	for _, pool := range configSpec.NodePoolList {
		if pool.Name == DefaultNodePoolName {
			var dataDiskList []*ackapi.DataDisk
			for _, dataDisk := range pool.DataDisk {
				dataDiskList = append(dataDiskList, &ackapi.DataDisk{
					Category:             tea.String(dataDisk.Category),
					Size:                 tea.Int64(dataDisk.Size),
					Encrypted:            tea.String(dataDisk.Encrypted),
					AutoSnapshotPolicyId: tea.String(dataDisk.AutoSnapshotPolicyID),
				})
			}
			nodePools = append(nodePools, &ackapi.Nodepool{
				AutoScaling: &ackapi.NodepoolAutoScaling{
					Enable:       tea.Bool(false),
					MaxInstances: tea.Int64(pool.InstancesNum),
					MinInstances: tea.Int64(pool.InstancesNum),
					Type:         tea.String(pool.ScalingType),
				},
				NodepoolInfo: &ackapi.NodepoolNodepoolInfo{
					Name: tea.String(pool.Name),
				},
				KubernetesConfig: &ackapi.NodepoolKubernetesConfig{
					Runtime:        tea.String(pool.Runtime),
					RuntimeVersion: tea.String(pool.RuntimeVersion),
				},
				ScalingGroup: &ackapi.NodepoolScalingGroup{
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
					DesiredSize:        tea.Int64(pool.InstancesNum),
				},
			})
			break
		}
	}
	req.Nodepools = nodePools
}

package ack

import (
	"encoding/json"
	"fmt"
	"strconv"

	ackapi "github.com/alibabacloud-go/cs-20151215/v5/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Create creates an upstream ACK cluster.
func Create(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec) error {
	err := validateCreateRequest(configSpec)
	if err != nil {
		return err
	}

	createClusterRequest := newClusterCreateRequest(configSpec)

	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters"
	request.Headers["Content-Type"] = "application/json"

	content, err := json.Marshal(createClusterRequest)
	if err != nil {
		return err
	}
	request.Content = content

	cluster := &clusterCreateResponse{}
	if err = ProcessRequest(client, request, cluster); err != nil {
		return err
	}
	configSpec.ClusterID = cluster.ClusterID
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

	// get worker creation info from default node pool
	getInitWorkerFromDefaultNodePool(configSpec, req)

	return req
}

func getInitWorkerFromDefaultNodePool(configSpec *ackv1.ACKClusterConfigSpec, req *ackapi.CreateClusterRequest) {
	nodePools := make([]*ackapi.Nodepool, 0, 1)
	for _, pool := range configSpec.NodePoolList {
		if pool.Name == DefaultNodePoolName {
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
				ScalingGroup: &ackapi.NodepoolScalingGroup{
					AutoRenew:          tea.Bool(pool.AutoRenew),
					AutoRenewPeriod:    tea.Int64(pool.AutoRenewPeriod),
					InstanceChargeType: tea.String(pool.InstanceChargeType),
					InstanceTypes:      tea.StringSlice(pool.InstanceTypes),
					KeyPair:            tea.String(pool.KeyPair),
					Period:             tea.Int64(pool.Period),
					PeriodUnit:         tea.String(pool.PeriodUnit),
					ImageType:          tea.String(pool.Platform),
					SystemDiskCategory: tea.String(pool.SystemDiskCategory),
					SystemDiskSize:     tea.Int64(pool.SystemDiskSize),
					VswitchIds:         tea.StringSlice(pool.VSwitchIds),
				},
			})
			break
		}
	}
	req.Nodepools = nodePools
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

// GetCluster returns cluster info
func GetCluster(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterDetailResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Headers["Content-Type"] = "application/json"
	request.Method = "GET"
	request.Scheme = "https"
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + state.ClusterID

	cluster := &ackapi.DescribeClusterDetailResponseBody{}
	if err := ProcessRequest(svc, request, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

// GetClusterWithParams returns cluster info map with params and output fields
func GetClusterWithParams(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) (*map[string]interface{}, error) {
	request := requests.NewCommonRequest()
	request.Headers["Content-Type"] = "application/json"
	request.Method = "GET"
	request.Scheme = "https"
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + state.ClusterID

	cluster := map[string]interface{}{}
	if err := ProcessRequest(svc, request, &cluster); err != nil {
		return nil, err
	}
	return &cluster, nil
}

// GetClusters returns cluster info by cluster name
func GetClusters(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClustersV1ResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "GET"
	request.Scheme = "https"
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v1/clusters"
	request.Headers["Content-Type"] = "application/json"
	request.QueryParams["name"] = state.Name

	request.Content = []byte(`{}`)

	clusterInfos := &ackapi.DescribeClustersV1ResponseBody{}

	if err := ProcessRequest(svc, request, clusterInfos); err != nil {
		return nil, err
	}
	return clusterInfos, nil
}

// RemoveCluster attempts to delete a cluster and retries the delete request if the cluster is busy.
func RemoveCluster(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec) error {
	return wait.ExponentialBackoff(backoff, func() (bool, error) {
		request := requests.NewCommonRequest()
		request.Method = "DELETE"
		request.Scheme = "https"
		request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
		request.Version = DefaultACKAPIVersion
		request.PathPattern = "/clusters/" + configSpec.ClusterID
		request.Headers["Content-Type"] = "application/json"

		request.Content = []byte(`{}`)

		_, err := client.ProcessCommonRequest(request)

		if err != nil {
			return false, err
		}
		return true, nil
	})
}

func DescribeTaskInfo(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeTaskInfoResponseBody, error) {
	request := requests.NewCommonRequest()

	request.Method = "GET"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/tasks/" + state.TaskId
	request.Headers["Content-Type"] = "application/json"

	taskInfoResponseBody := &ackapi.DescribeTaskInfoResponseBody{}
	if err := ProcessRequest(svc, request, taskInfoResponseBody); err != nil {
		return nil, err
	}
	return taskInfoResponseBody, nil
}

func UpgradeCluster(svc *sdk.Client, upstreamSpec *ackv1.ACKClusterConfigSpec) (*ackapi.UpgradeClusterResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + upstreamSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v2/clusters/" + upstreamSpec.ClusterID + "/upgrade"
	request.Headers["Content-Type"] = "application/json"

	upgradeClusterRequest := &ackapi.UpgradeClusterRequest{
		NextVersion: &upstreamSpec.KubernetesVersion,
	}
	content, err := json.Marshal(upgradeClusterRequest)
	if err != nil {
		return nil, err
	}
	request.Content = content
	upgradeClusterResponseBody := &ackapi.UpgradeClusterResponseBody{}
	if err = ProcessRequest(svc, request, upgradeClusterResponseBody); err != nil {
		return nil, err
	}
	return upgradeClusterResponseBody, nil
}

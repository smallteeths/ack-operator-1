package ack

import (
	"encoding/json"
	"fmt"
	"strconv"

	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"

	ackapi "github.com/alibabacloud-go/cs-20151215/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
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
	req.RegionId = tea.String(configSpec.RegionID)
	req.KubernetesVersion = tea.String(configSpec.KubernetesVersion)
	req.Vpcid = tea.String(configSpec.VpcID)
	req.ZoneId = tea.String(configSpec.ZoneID)
	req.ContainerCidr = tea.String(configSpec.ContainerCidr)
	req.ServiceCidr = tea.String(configSpec.ServiceCidr)
	req.NodeCidrMask = tea.String(strconv.Itoa(int(configSpec.NodeCidrMask)))
	req.CloudMonitorFlags = tea.Bool(configSpec.CloudMonitorFlags)
	req.SnatEntry = tea.Bool(configSpec.SnatEntry)
	req.ProxyMode = tea.String(configSpec.ProxyMode)
	req.EndpointPublicAccess = tea.Bool(configSpec.EndpointPublicAccess)
	req.SecurityGroupId = tea.String(configSpec.SecurityGroupID)
	req.SshFlags = tea.Bool(configSpec.SSHFlags)
	req.OsType = tea.String(configSpec.OsType)
	req.Platform = tea.String(configSpec.Platform)
	req.DisableRollback = tea.Bool(configSpec.DisableRollback)
	req.LoginPassword = tea.String(configSpec.LoginPassword)
	req.KeyPair = tea.String(configSpec.KeyPair)
	// req.VswitchIds = tea.StringSlice(configSpec.VswitchIds)
	// master instance
	req.MasterCount = tea.Int64(configSpec.MasterCount)
	req.MasterVswitchIds = tea.StringSlice(configSpec.MasterVswitchIds)
	req.MasterInstanceTypes = tea.StringSlice(configSpec.MasterInstanceTypes)
	req.MasterInstanceChargeType = tea.String(configSpec.MasterInstanceChargeType)
	req.MasterPeriod = tea.Int64(configSpec.MasterPeriod)
	req.MasterPeriodUnit = tea.String(configSpec.MasterPeriodUnit)
	req.MasterAutoRenew = tea.Bool(configSpec.MasterAutoRenew)
	req.MasterAutoRenewPeriod = tea.Int64(configSpec.MasterAutoRenewPeriod)
	req.MasterSystemDiskCategory = tea.String(configSpec.MasterSystemDiskCategory)
	req.MasterSystemDiskSize = tea.Int64(configSpec.MasterSystemDiskSize)

	// get worker creation info from default node pool
	getInitWorkerFromDefaultNodePool(configSpec, req)

	return req
}

func getInitWorkerFromDefaultNodePool(configSpec *ackv1.ACKClusterConfigSpec, req *ackapi.CreateClusterRequest) {
	for _, pool := range configSpec.NodePoolList {
		if pool.Name == DefaultNodePoolName {
			req.NumOfNodes = tea.Int64(pool.InstancesNum)
			req.WorkerVswitchIds = tea.StringSlice(pool.VSwitchIds)
			req.WorkerInstanceTypes = tea.StringSlice(pool.InstanceTypes)
			req.WorkerInstanceChargeType = tea.String(pool.InstanceChargeType)
			req.WorkerPeriod = tea.Int64(pool.Period)
			req.WorkerPeriodUnit = tea.String(pool.PeriodUnit)
			req.WorkerAutoRenew = tea.Bool(pool.AutoRenew)
			req.WorkerAutoRenewPeriod = tea.Int64(pool.AutoRenewPeriod)
			req.WorkerSystemDiskCategory = tea.String(pool.SystemDiskCategory)
			req.WorkerSystemDiskSize = tea.Int64(pool.SystemDiskSize)
			req.Platform = tea.String(pool.Platform)

			break
		}
	}
}

// validateCreateRequest checks a config for the ability to generate a create request
func validateCreateRequest(configSpec *ackv1.ACKClusterConfigSpec) error {
	if configSpec.Name == "" {
		return fmt.Errorf("cluster display name is required")
	} else if configSpec.RegionID == "" {
		return fmt.Errorf("region id is required")
	} else if configSpec.LoginPassword == "" && configSpec.KeyPair == "" {
		return fmt.Errorf("either login password or key pair name is needed")
	} else if configSpec.VpcID == "" && !configSpec.SnatEntry {
		return fmt.Errorf("snat entry is required when vpc is auto created")
	}

	return nil
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

func GetUpgradeStatus(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) (*ackapi.GetUpgradeStatusResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "GET"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v2/clusters/" + state.ClusterID + "/upgrade/status"
	request.Headers["Content-Type"] = "application/json"

	upgradeStatusResponseBody := &ackapi.GetUpgradeStatusResponseBody{}
	if err := ProcessRequest(svc, request, upgradeStatusResponseBody); err != nil {
		return nil, err
	}
	return upgradeStatusResponseBody, nil
}

func UpgradeCluster(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec, upstreamSpec *ackv1.ACKClusterConfigSpec) error {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v2/clusters/" + state.ClusterID + "/upgrade"
	request.Headers["Content-Type"] = "application/json"

	upgradeClusterRequest := &ackapi.UpgradeClusterRequest{
		NextVersion: tea.String(state.KubernetesVersion),
		Version:     tea.String(upstreamSpec.KubernetesVersion),
	}
	content, err := json.Marshal(upgradeClusterRequest)
	if err != nil {
		return err
	}
	request.Content = content
	_, err = svc.ProcessCommonRequest(request)
	return err
}

func PauseUpgradeStatus(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) error {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v2/clusters/" + state.ClusterID + "/upgrade/pause"
	request.Headers["Content-Type"] = "application/json"
	_, err := svc.ProcessCommonRequest(request)
	return err
}

func ResumeUpgradeStatus(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) error {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v2/clusters/" + state.ClusterID + "/upgrade/resume"
	request.Headers["Content-Type"] = "application/json"
	_, err := svc.ProcessCommonRequest(request)
	return err
}

func CancelUpgradeStatus(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) error {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https" // https | http
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/api/v2/clusters/" + state.ClusterID + "/upgrade/cancel"
	request.Headers["Content-Type"] = "application/json"
	_, err := svc.ProcessCommonRequest(request)
	return err
}

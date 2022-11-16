package ack

import (
	"encoding/json"
	"fmt"
	"strings"

	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"

	ackapi "github.com/alibabacloud-go/cs-20151215/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

func newNodePoolCreateRequest(npConfig *ackv1.NodePoolInfo) *ackapi.CreateClusterNodePoolRequest {
	var dataDiskList []*ackapi.DataDisk
	for _, dataDisk := range npConfig.DataDisk {
		dataDiskList = append(dataDiskList, &ackapi.DataDisk{
			Category:             tea.String(dataDisk.Category),
			Size:                 tea.Int64(dataDisk.Size),
			Encrypted:            tea.String(dataDisk.Encrypted),
			AutoSnapshotPolicyId: tea.String(dataDisk.AutoSnapshotPolicyID),
		})
	}

	return &ackapi.CreateClusterNodePoolRequest{
		AutoScaling: &ackapi.CreateClusterNodePoolRequestAutoScaling{
			Enable:                tea.Bool(false),
			MaxInstances:          tea.Int64(npConfig.InstancesNum),
			MinInstances:          tea.Int64(npConfig.InstancesNum),
			Type:                  tea.String(npConfig.ScalingType),
			IsBondEip:             tea.Bool(npConfig.IsBondEip),
			EipInternetChargeType: tea.String(npConfig.EipInternetChargeType),
			EipBandwidth:          tea.Int64(npConfig.EipBandwidth),
		},
		NodepoolInfo: &ackapi.CreateClusterNodePoolRequestNodepoolInfo{
			Name: tea.String(npConfig.Name),
		},
		ScalingGroup: &ackapi.CreateClusterNodePoolRequestScalingGroup{
			AutoRenew:          tea.Bool(npConfig.AutoRenew),
			AutoRenewPeriod:    tea.Int64(npConfig.AutoRenewPeriod),
			DataDisks:          dataDiskList,
			InstanceChargeType: tea.String(npConfig.InstanceChargeType),
			InstanceTypes:      tea.StringSlice(npConfig.InstanceTypes),
			KeyPair:            tea.String(npConfig.KeyPair),
			Period:             tea.Int64(npConfig.Period),
			PeriodUnit:         tea.String(npConfig.PeriodUnit),
			Platform:           tea.String(npConfig.Platform),
			SystemDiskCategory: tea.String(npConfig.SystemDiskCategory),
			SystemDiskSize:     tea.Int64(npConfig.SystemDiskSize),
			VswitchIds:         tea.StringSlice(npConfig.VSwitchIds),
		},
	}
}

func UpdateNodePoolBatch(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec) (Status, error) {
	// get newest node pool info
	nodePools, err := GetNodePools(client, configSpec)
	if err != nil {
		return NotChanged, err
	}
	nodePoolsInfo, convErr := ToNodePoolConfigInfo(nodePools)
	if convErr != nil {
		return NotChanged, err
	}

	flag := NotChanged

	nodePoolNameKeyMap := make(map[string]ackv1.NodePoolInfo)
	upstreamNodePoolInfoMap := make(map[string]ackv1.NodePoolInfo)
	for _, np := range nodePoolsInfo {
		upstreamNodePoolInfoMap[np.NodepoolId] = np
		nodePoolNameKeyMap[np.Name] = np
	}
	// fix node pool id is empty after created
	for i, info := range configSpec.NodePoolList {
		if nodePool, ok := nodePoolNameKeyMap[info.Name]; ok {
			// `platform` won't change when edit `instanceNum`
			if info.Platform == nodePool.Platform {
				configSpec.NodePoolList[i].NodepoolId = nodePool.NodepoolId
			}
		}
	}

	var (
		updateQueue []ackv1.NodePoolInfo
		createQueue []ackv1.NodePoolInfo
	)
	currentConfigPool := configSpec.NodePoolList
	for _, info := range currentConfigPool {
		if info.NodepoolId != "" {
			updateQueue = append(updateQueue, *info.DeepCopy())
		} else {
			// default node pool will auto create while creating
			if info.Name == DefaultNodePoolName {
				continue
			}
			createQueue = append(createQueue, *info.DeepCopy())
		}
	}

	// create node pool
	var failedMsg []string
	for _, np := range createQueue {
		c, err := CreateNodePool(client, configSpec, &np)
		if err != nil {
			if strings.Contains(err.Error(), "is already exist in cluster") {
				continue
			} else {
				return Changed, err
			}
		}

		for j, old := range configSpec.NodePoolList {
			if old.Name == np.Name {
				configSpec.NodePoolList[j].NodepoolId = tea.StringValue(c.NodepoolId)
			}
		}
		np.NodepoolId = tea.StringValue(c.NodepoolId)
		flag = Changed
		_, errMsg := ScaleUpNodePool(client, configSpec, &np, np.InstancesNum)
		if errMsg != nil {
			if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
				failedMsg = append(failedMsg, fmt.Sprintf("%s(scale up error:%s)", np.NodepoolId, errMsg.Error()))
			}
			continue
		}
	}

	// update node pool
	for _, np := range updateQueue {
		unp, ok := upstreamNodePoolInfoMap[np.NodepoolId]
		if !ok {
			continue
		}
		if unp.InstancesNum != np.InstancesNum {
			if unp.InstancesNum < np.InstancesNum {
				flag = Changed
				_, errMsg := ScaleUpNodePool(client, configSpec, &np, np.InstancesNum-unp.InstancesNum)
				if errMsg != nil {
					if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
						failedMsg = append(failedMsg, fmt.Sprintf("%s(scale up error:%s)", np.NodepoolId, errMsg.Error()))
					}
					continue
				}
			} else {
				nodes, errMsg := GetClusterNodes(client, configSpec, np.NodepoolId)
				if errMsg != nil {
					if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
						failedMsg = append(failedMsg, fmt.Sprintf("%s(scale down query error:%s)", np.NodepoolId, errMsg.Error()))
					}
					continue
				}
				if len(nodes.Nodes) == int(np.InstancesNum) {
					continue
				}
				scaleDownNum := unp.InstancesNum - np.InstancesNum
				var nodeNames []string
				for i := 0; i < int(scaleDownNum); i++ {
					nodeNames = append(nodeNames, *nodes.Nodes[i].NodeName)
				}
				flag = Changed
				errMsg = ScaleDownNodePool(client, configSpec, nodeNames)
				if errMsg != nil {
					// DeleteClusterNodes DO NOT returns any task-id or process info,
					// err message `cannot operate cluster where state is removing` means cluster is removing nodes
					if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
						failedMsg = append(failedMsg, fmt.Sprintf("%s(scale down error:%s)", np.NodepoolId, errMsg.Error()))
					}
					continue
				}
			}
		}
	}

	// delete node pool
	updatedIdMap := make(map[string]string)
	for _, poolInfo := range updateQueue {
		updatedIdMap[poolInfo.NodepoolId] = poolInfo.NodepoolId
	}
	for _, np := range nodePoolsInfo {
		npId := np.NodepoolId
		_, ok := updatedIdMap[npId]
		if !ok && np.Name != DefaultNodePoolName {
			flag = Changed
			nodes, err := GetClusterNodes(client, configSpec, npId)
			if err != nil {
				return Changed, err
			}
			if len(nodes.Nodes) > 0 {
				var npNames []string
				for _, node := range nodes.Nodes {
					npNames = append(npNames, tea.StringValue(node.NodeName))
				}
				err = ScaleDownNodePool(client, configSpec, npNames)
				if err != nil {
					// DeleteClusterNodes DO NOT returns any task-id or process info,
					// err message `cannot operate cluster where state is removing` means cluster is removing nodes
					if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
						failedMsg = append(failedMsg, fmt.Sprintf("%s(scale down error:%s)", npId, err.Error()))
					}
				}
			}

			_, err = DeleteNodePool(client, configSpec, npId)
			if err != nil {
				if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
					failedMsg = append(failedMsg, fmt.Sprintf("%s(delete nood pool error:%s)", npId, err.Error()))
				}
			}
		}
	}

	// update node pool name
	for _, np := range configSpec.NodePoolList {
		if nodePool, ok := upstreamNodePoolInfoMap[np.NodepoolId]; ok && nodePool.Name != np.Name {
			if _, err := UpdateNodePool(client, configSpec, &np); err != nil {
				flag = Changed
				if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
					failedMsg = append(failedMsg, fmt.Sprintf("%s(update nood pool name error:%s)", np.NodepoolId, err.Error()))
				}
			}
		}
	}

	if len(failedMsg) > 0 {
		return Changed, fmt.Errorf("%s", strings.Join(failedMsg, ";"))
	}

	return flag, nil
}

func GetNodePools(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterNodePoolsResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "GET"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodepools"
	request.Headers["Content-Type"] = "application/json"

	body := `{}`
	request.Content = []byte(body)

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		return nil, err
	}

	nodePoolInfo := &ackapi.DescribeClusterNodePoolsResponseBody{}
	if err = json.Unmarshal(response.GetHttpContentBytes(), nodePoolInfo); err != nil {
		return nil, err
	}
	return nodePoolInfo, nil
}

func CreateNodePool(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec, npConfig *ackv1.NodePoolInfo) (*ackapi.CreateClusterNodePoolResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodepools"
	request.Headers["Content-Type"] = "application/json"

	body, err := json.Marshal(newNodePoolCreateRequest(npConfig))
	if err != nil {
		return nil, err
	}
	request.Content = body

	resp := &ackapi.CreateClusterNodePoolResponseBody{}
	if err = ProcessRequest(client, request, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func DeleteNodePool(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec, nodePoolId string) (*ackapi.DeleteClusterNodepoolResponse, error) {
	request := requests.NewCommonRequest()
	request.Method = "DELETE"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodepools/" + nodePoolId
	request.Headers["Content-Type"] = "application/json"

	request.Content = []byte(`{}`)

	resp := &ackapi.DeleteClusterNodepoolResponse{}
	if err := ProcessRequest(client, request, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func UpdateNodePool(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec, npConfig *ackv1.NodePoolInfo) (*ackapi.ModifyClusterNodePoolResponse, error) {
	request := requests.NewCommonRequest()
	request.Method = "PUT"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodepools/" + npConfig.NodepoolId
	request.Headers["Content-Type"] = "application/json"

	var err error
	request.Content, err = json.Marshal(ackapi.ModifyClusterNodePoolRequest{
		NodepoolInfo: &ackapi.ModifyClusterNodePoolRequestNodepoolInfo{
			Name: tea.String(npConfig.Name),
		},
	})
	if err != nil {
		return nil, err
	}
	resp := &ackapi.ModifyClusterNodePoolResponse{}
	if err = ProcessRequest(client, request, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func ScaleUpNodePool(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec, npConfig *ackv1.NodePoolInfo, upNum int64) (*ackapi.ModifyClusterNodePoolResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodepools/" + npConfig.NodepoolId
	request.Headers["Content-Type"] = "application/json"

	var err error
	request.Content, err = json.Marshal(ackapi.ScaleClusterNodePoolRequest{
		Count: tea.Int64(upNum),
	})
	if err != nil {
		return nil, err
	}
	resp := &ackapi.ModifyClusterNodePoolResponseBody{}
	err = ProcessRequest(client, request, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func ScaleDownNodePool(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec, nodeNames []string) error {
	request := requests.NewCommonRequest()
	request.Method = "POST"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodes"
	request.Headers["Content-Type"] = "application/json"

	var err error
	request.Content, err = json.Marshal(ackapi.DeleteClusterNodesRequest{
		DrainNode:   tea.Bool(true),
		ReleaseNode: tea.Bool(true),
		Nodes:       tea.StringSlice(nodeNames),
	})
	if err != nil {
		return err
	}
	return ProcessRequest(client, request, nil)
}

func GetClusterNodes(client *sdk.Client, configSpec *ackv1.ACKClusterConfigSpec, npId string) (*ackapi.DescribeClusterNodesResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "GET"
	request.Scheme = "https"
	request.Domain = "cs." + configSpec.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/clusters/" + configSpec.ClusterID + "/nodes"
	request.Headers["Content-Type"] = "application/json"
	request.QueryParams["nodepool_id"] = npId

	request.Content = []byte(`{}`)

	resp := &ackapi.DescribeClusterNodesResponseBody{}
	err := ProcessRequest(client, request, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func ToNodePoolConfigInfo(nodePoolInfo *ackapi.DescribeClusterNodePoolsResponseBody) ([]ackv1.NodePoolInfo, error) {
	var nodePoolList []ackv1.NodePoolInfo
	for _, nodePool := range nodePoolInfo.Nodepools {
		var dataDisks []ackv1.DiskInfo
		if nodePool.ScalingGroup.DataDisks != nil {
			for _, disk := range nodePool.ScalingGroup.DataDisks {
				dataDisks = append(dataDisks, ackv1.DiskInfo{
					Category:             tea.StringValue(disk.Category),
					Size:                 tea.Int64Value(disk.Size),
					Encrypted:            tea.StringValue(disk.Encrypted),
					AutoSnapshotPolicyID: tea.StringValue(disk.AutoSnapshotPolicyId),
				})
			}
		}
		nodePoolList = append(nodePoolList, ackv1.NodePoolInfo{
			NodepoolId:            tea.StringValue(nodePool.NodepoolInfo.NodepoolId),
			Name:                  tea.StringValue(nodePool.NodepoolInfo.Name),
			InstancesNum:          tea.Int64Value(nodePool.Status.TotalNodes),
			ScalingType:           tea.StringValue(nodePool.AutoScaling.Type),
			IsBondEip:             tea.BoolValue(nodePool.AutoScaling.IsBondEip),
			EipInternetChargeType: tea.StringValue(nodePool.AutoScaling.EipInternetChargeType),
			EipBandwidth:          tea.Int64Value(nodePool.AutoScaling.EipBandwidth),
			/* scaling_group */
			AutoRenew:          tea.BoolValue(nodePool.ScalingGroup.AutoRenew),
			AutoRenewPeriod:    tea.Int64Value(nodePool.ScalingGroup.AutoRenewPeriod),
			DataDisk:           dataDisks,
			InstanceChargeType: tea.StringValue(nodePool.ScalingGroup.InstanceChargeType),
			InstanceTypes:      tea.StringSliceValue(nodePool.ScalingGroup.InstanceTypes),
			KeyPair:            tea.StringValue(nodePool.ScalingGroup.KeyPair),
			Period:             tea.Int64Value(nodePool.ScalingGroup.Period),
			PeriodUnit:         tea.StringValue(nodePool.ScalingGroup.PeriodUnit),
			Platform:           tea.StringValue(nodePool.ScalingGroup.Platform),
			SystemDiskCategory: tea.StringValue(nodePool.ScalingGroup.SystemDiskCategory),
			SystemDiskSize:     tea.Int64Value(nodePool.ScalingGroup.SystemDiskSize),
			VSwitchIds:         tea.StringSliceValue(nodePool.ScalingGroup.VswitchIds),
		})
	}
	return nodePoolList, nil
}

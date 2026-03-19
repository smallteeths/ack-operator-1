package ack

import (
	"fmt"
	"strings"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"github.com/sirupsen/logrus"
)

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
			MinInstances:       nodePool.AutoScaling.MinInstances,
			MaxInstances:       nodePool.AutoScaling.MaxInstances,
			AutoScalingEnabled: nodePool.AutoScaling.Enable,
			AutoRenew:          tea.BoolValue(nodePool.ScalingGroup.AutoRenew),
			AutoRenewPeriod:    tea.Int64Value(nodePool.ScalingGroup.AutoRenewPeriod),
			DataDisk:           dataDisks,
			InstanceChargeType: tea.StringValue(nodePool.ScalingGroup.InstanceChargeType),
			InstanceTypes:      tea.StringSliceValue(nodePool.ScalingGroup.InstanceTypes),
			KeyPair:            tea.StringValue(nodePool.ScalingGroup.KeyPair),
			Period:             tea.Int64Value(nodePool.ScalingGroup.Period),
			PeriodUnit:         tea.StringValue(nodePool.ScalingGroup.PeriodUnit),
			Platform:           tea.StringValue(nodePool.ScalingGroup.ImageType),
			SystemDiskCategory: tea.StringValue(nodePool.ScalingGroup.SystemDiskCategory),
			SystemDiskSize:     tea.Int64Value(nodePool.ScalingGroup.SystemDiskSize),
			VSwitchIds:         tea.StringSliceValue(nodePool.ScalingGroup.VswitchIds),
			Runtime:            tea.StringValue(nodePool.KubernetesConfig.Runtime),
			RuntimeVersion:     tea.StringValue(nodePool.KubernetesConfig.RuntimeVersion),
		})
	}
	return nodePoolList, nil
}

func BatchUpdateClusterNodePools(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) (Status, error) {
	// 获取当前的 nodepool 信息，对比 configSpec 上面的 NodePoolList 看是否需要更新 nodepool
	nodePools, err := DescribeClusterNodePools(client, configSpec)
	if err != nil {
		return NotChanged, err
	}
	nodePoolsInfo, convErr := ToNodePoolConfigInfo(nodePools)
	if convErr != nil {
		return NotChanged, convErr
	}

	flag := NotChanged
	nodePoolNameKeyMap := make(map[string]ackv1.NodePoolInfo)
	upstreamNodePoolInfoMap := make(map[string]ackv1.NodePoolInfo)

	for _, np := range nodePoolsInfo {
		if np.NodepoolId != "" {
			upstreamNodePoolInfoMap[np.NodepoolId] = np
		}
		if np.Name != "" {
			nodePoolNameKeyMap[np.Name] = np
		}
	}

	// 本地 spec 中 NodepoolId 为空时，根据 name + platform 从上游补齐
	for i, info := range configSpec.NodePoolList {
		if nodePool, ok := nodePoolNameKeyMap[info.Name]; ok {
			// platform 相同才认为是同一个 nodepool
			if info.Platform == nodePool.Platform {
				configSpec.NodePoolList[i].NodepoolId = nodePool.NodepoolId
			}
		}
	}

	// 根据是否有 NodepoolId 划分要 create / update 的队列
	var (
		updateQueue []ackv1.NodePoolInfo
		createQueue []ackv1.NodePoolInfo
	)

	currentConfigPool := configSpec.NodePoolList
	for _, info := range currentConfigPool {
		if info.NodepoolId != "" {
			updateQueue = append(updateQueue, *info.DeepCopy())
		} else {
			createQueue = append(createQueue, *info.DeepCopy())
		}
	}
	var failedMsg []string

	// 创建 nodepool
	for _, np := range createQueue {
		c, err := CreateClusterNodePool(client, configSpec, &np)
		if err != nil {
			if strings.Contains(err.Error(), "is already exist in cluster") {
				// 已存在则跳过
				continue
			}
			return Changed, err
		}
		// 回填 NodepoolId 到 configSpec
		for j, old := range configSpec.NodePoolList {
			if old.Name == np.Name {
				configSpec.NodePoolList[j].NodepoolId = tea.StringValue(c.NodepoolId)
			}
		}
		np.NodepoolId = tea.StringValue(c.NodepoolId)
		flag = Changed
	}
	// 更新 nodepool只处理实例数变化，和 autoscaling 比昂话
	for _, np := range updateQueue {
		unp, ok := upstreamNodePoolInfoMap[np.NodepoolId]
		if !ok {
			continue
		}
		// 配置变更，调用 ModifyClusterNodePool 修改自动扩容
		if autoscalingChanged(unp, np) {
			flag = Changed
			minIns, maxIns := effectiveMinMax(np)
			enable := autoscalingEnabled(np)
			req := &ackapi.ModifyClusterNodePoolRequest{
				AutoScaling: &ackapi.ModifyClusterNodePoolRequestAutoScaling{
					Enable:       tea.Bool(enable),
					MinInstances: tea.Int64(minIns),
					MaxInstances: tea.Int64(maxIns),
				},
			}
			headers := map[string]*string{}
			runtime := &util.RuntimeOptions{}
			_, errMsg := client.ModifyClusterNodePoolWithOptions(tea.String(configSpec.ClusterID), tea.String(np.NodepoolId), req, headers, runtime)
			if errMsg != nil {
				if !isThrottlingError(errMsg) && !isUnexpectedStatusError(errMsg) {
					failedMsg = append(failedMsg, fmt.Sprintf("%s(modify autoscaling error:%s)", np.NodepoolId, errMsg.Error()))
				}
				// autoscaling 修改失败就继续处理下一个
				continue
			}
			if enable {
				continue
			}
		}
		// 若 autoscaling 开启：直接跳过 InstancesNum 的 scale 逻辑
		if autoscalingEnabled(np) {
			continue
		}
		// InstancesNum scale
		if unp.InstancesNum == np.InstancesNum {
			continue
		}
		// scale up
		if unp.InstancesNum < np.InstancesNum {
			flag = Changed
			scaleUp := np.InstancesNum - unp.InstancesNum

			_, errMsg := ScaleClusterNodePool(client, configSpec, &np, scaleUp)
			if errMsg != nil {
				if !isThrottlingError(errMsg) && !isUnexpectedStatusError(errMsg) {
					failedMsg = append(failedMsg, fmt.Sprintf("%s(scale up error:%s)", np.NodepoolId, errMsg.Error()))
				}
				continue
			}
			continue
		}
		// scale down
		nodePool, errMsg := DescribeClusterNodesByNodePool(client, configSpec, np.NodepoolId)
		if errMsg != nil {
			if !isThrottlingError(errMsg) && !isUnexpectedStatusError(errMsg) {
				failedMsg = append(failedMsg, fmt.Sprintf("%s(scale down query error:%s)", np.NodepoolId, errMsg.Error()))
			}
			continue
		}
		// 如果当前节点数已经等于期望值，就不再操作
		if len(nodePool.Nodes) == int(np.InstancesNum) {
			continue
		}
		scaleDownNum := unp.InstancesNum - np.InstancesNum
		if scaleDownNum <= 0 {
			continue
		}
		// 获取当前 scaleDownNum 个节点名
		var nodeNames []string
		for i := 0; i < int(scaleDownNum) && i < len(nodePool.Nodes); i++ {
			if nodePool.Nodes[i].NodeName != nil {
				nodeNames = append(nodeNames, *nodePool.Nodes[i].NodeName)
			}
		}
		if len(nodeNames) == 0 {
			continue
		}
		flag = Changed
		errMsg = DeleteClusterNodes(client, configSpec, nodeNames)
		if errMsg != nil {
			// DeleteClusterNodes 不返回 task id，错误 "cannot operate cluster where state is removing"
			// 表示集群正在移除节点，这里只忽略“节流/unexpected status”之外的错误
			if !isThrottlingError(errMsg) && !isUnexpectedStatusError(errMsg) {
				failedMsg = append(failedMsg, fmt.Sprintf("%s(scale down error:%s)", np.NodepoolId, errMsg.Error()))
			}
			continue
		}
	}

	// 删除多余的 nodepool
	updatedIdSet := make(map[string]struct{}, len(updateQueue))
	for _, poolInfo := range updateQueue {
		if poolInfo.NodepoolId != "" {
			updatedIdSet[poolInfo.NodepoolId] = struct{}{}
		}
	}

	for _, np := range nodePoolsInfo {
		npId := np.NodepoolId
		if npId == "" {
			continue
		}

		if _, ok := updatedIdSet[npId]; ok {
			continue
		}
		// 本地 spec 中已经没有了这个 nodepool，需要删除
		flag = Changed
		// 如果 autoscaling 开启，删除时需要先关闭
		if up, ok := upstreamNodePoolInfoMap[npId]; ok && autoscalingEnabled(up) {
			req := &ackapi.ModifyClusterNodePoolRequest{
				AutoScaling: &ackapi.ModifyClusterNodePoolRequestAutoScaling{
					Enable: tea.Bool(false),
				},
			}
			headers := map[string]*string{}
			runtime := &util.RuntimeOptions{}

			_, err := client.ModifyClusterNodePoolWithOptions(
				tea.String(configSpec.ClusterID),
				tea.String(npId),
				req,
				headers,
				runtime,
			)
			if err != nil {
				// 关闭失败不直接 return，记录后继续删
				if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
					failedMsg = append(failedMsg, fmt.Sprintf("%s(disable autoscaling error:%s)", npId, err.Error()))
				}
			}
		}
		// 查询该 nodepool 下节点并删除
		nodes, err := DescribeClusterNodesByNodePool(client, configSpec, npId)
		if err != nil {
			return Changed, err
		}
		if len(nodes.Nodes) > 0 {
			var npNames []string
			for _, node := range nodes.Nodes {
				if node.NodeName != nil {
					npNames = append(npNames, tea.StringValue(node.NodeName))
				}
			}
			if len(npNames) > 0 {
				err = DeleteClusterNodes(client, configSpec, npNames)
				if err != nil {
					if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
						failedMsg = append(failedMsg, fmt.Sprintf("%s(scale down error:%s)", npId, err.Error()))
					}
				}
			}
		}
		// 删除 nodepool
		_, err = DeleteClusterNodePool(client, configSpec, npId)
		if err != nil {
			if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
				failedMsg = append(failedMsg, fmt.Sprintf("%s(delete node pool error:%s)", npId, err.Error()))
			}
		}
	}
	// 更新 nodepool 名称
	for _, np := range configSpec.NodePoolList {
		if np.NodepoolId == "" {
			continue
		}
		if nodePool, ok := upstreamNodePoolInfoMap[np.NodepoolId]; ok && nodePool.Name != np.Name {
			if _, err := UpdateClusterNodePool(client, configSpec, &np); err != nil {
				flag = Changed
				if !isThrottlingError(err) && !isUnexpectedStatusError(err) {
					failedMsg = append(failedMsg, fmt.Sprintf("%s(update node pool name error:%s)", np.NodepoolId, err.Error()))
				}
			}
		}
	}

	// 聚合错误信息
	if len(failedMsg) > 0 {
		return Changed, fmt.Errorf("%s", strings.Join(failedMsg, ";"))
	}

	return flag, nil
}

// CreateClusterNodePool
func CreateClusterNodePool(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec, npConfig *ackv1.NodePoolInfo) (*ackapi.CreateClusterNodePoolResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	if npConfig == nil {
		return nil, fmt.Errorf("node pool config is nil")
	}
	req := newNodePoolCreateRequest(client, configSpec, npConfig)
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.CreateClusterNodePoolWithOptions(
		tea.String(configSpec.ClusterID),
		req,
		headers,
		runtime,
	)
	if err != nil {
		return nil, fmt.Errorf("create ACK cluster node pool failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("create ACK cluster node pool succeeded but response body is nil")
	}
	return resp.Body, nil
}

// ScaleClusterNodePool
func ScaleClusterNodePool(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec, npConfig *ackv1.NodePoolInfo, count int64) (*ackapi.ScaleClusterNodePoolResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	if npConfig == nil {
		return nil, fmt.Errorf("node pool config is nil")
	}
	if npConfig.NodepoolId == "" {
		return nil, fmt.Errorf("nodepoolId is empty")
	}
	req := &ackapi.ScaleClusterNodePoolRequest{
		Count: tea.Int64(count),
	}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.ScaleClusterNodePoolWithOptions(
		tea.String(configSpec.ClusterID),
		tea.String(npConfig.NodepoolId),
		req,
		headers,
		runtime,
	)
	if err != nil {
		return nil, fmt.Errorf("scale ACK cluster node pool failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("scale ACK cluster node pool succeeded but response body is nil")
	}
	return resp.Body, nil
}

// DescribeClusterNodesByNodePool
func DescribeClusterNodesByNodePool(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec, nodePoolID string) (*ackapi.DescribeClusterNodesResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	if nodePoolID == "" {
		return nil, fmt.Errorf("nodePoolID is empty")
	}
	req := &ackapi.DescribeClusterNodesRequest{
		NodepoolId: tea.String(nodePoolID),
	}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.DescribeClusterNodesWithOptions(tea.String(configSpec.ClusterID), req, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("describe ACK cluster nodes failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("describe ACK cluster nodes succeeded but response body is nil")
	}

	return resp.Body, nil
}

// DescribeClusterNodesByNodePool
func DeleteClusterNodes(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec, nodeNames []string) error {
	if configSpec == nil {
		return fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return fmt.Errorf("clusterID is empty")
	}
	if len(nodeNames) == 0 {
		return fmt.Errorf("nodeNames is empty")
	}
	req := &ackapi.DeleteClusterNodesRequest{
		DrainNode:   tea.Bool(true),
		ReleaseNode: tea.Bool(true),
		Nodes:       tea.StringSlice(nodeNames),
	}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	_, err := client.DeleteClusterNodesWithOptions(tea.String(configSpec.ClusterID), req, headers, runtime)
	if err != nil {
		return fmt.Errorf("delete ACK cluster nodes failed: %w", err)
	}

	return nil
}

// DeleteClusterNodePool
func DeleteClusterNodePool(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec, nodePoolID string) (*ackapi.DeleteClusterNodepoolResponse, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	if nodePoolID == "" {
		return nil, fmt.Errorf("nodePoolID is empty")
	}
	// 强制删除
	req := &ackapi.DeleteClusterNodepoolRequest{
		Force: tea.Bool(true),
	}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.DeleteClusterNodepoolWithOptions(tea.String(configSpec.ClusterID), tea.String(nodePoolID), req, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("delete ACK cluster node pool failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("delete ACK cluster node pool succeeded but response/body is nil")
	}

	return resp, nil
}

// UpdateClusterNodePool
func UpdateClusterNodePool(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec, npConfig *ackv1.NodePoolInfo) (*ackapi.ModifyClusterNodePoolResponse, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	if npConfig == nil {
		return nil, fmt.Errorf("node pool config is nil")
	}
	if npConfig.NodepoolId == "" {
		return nil, fmt.Errorf("nodepoolId is empty")
	}
	req := &ackapi.ModifyClusterNodePoolRequest{
		NodepoolInfo: &ackapi.ModifyClusterNodePoolRequestNodepoolInfo{
			Name: tea.String(npConfig.Name),
		},
	}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.ModifyClusterNodePoolWithOptions(tea.String(configSpec.ClusterID), tea.String(npConfig.NodepoolId), req, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("modify ACK cluster node pool failed: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("modify ACK cluster node pool succeeded but response is nil")
	}

	return resp, nil
}

func newNodePoolCreateRequest(
	client *ackapi.Client,
	configSpec *ackv1.ACKClusterConfigSpec,
	npConfig *ackv1.NodePoolInfo,
) *ackapi.CreateClusterNodePoolRequest {
	var dataDiskList []*ackapi.DataDisk
	for _, dataDisk := range npConfig.DataDisk {
		dataDiskList = append(dataDiskList, &ackapi.DataDisk{
			Category:             tea.String(dataDisk.Category),
			Size:                 tea.Int64(dataDisk.Size),
			Encrypted:            tea.String(dataDisk.Encrypted),
			AutoSnapshotPolicyId: tea.String(dataDisk.AutoSnapshotPolicyID),
		})
	}

	enable := false
	minIns := npConfig.InstancesNum
	maxIns := npConfig.InstancesNum
	scalingType := npConfig.ScalingType

	if npConfig.AutoScalingEnabled != nil && *npConfig.AutoScalingEnabled {
		enable = true
		if npConfig.MinInstances != nil {
			minIns = *npConfig.MinInstances
		}
		if npConfig.MaxInstances != nil {
			maxIns = *npConfig.MaxInstances
		}
	}

	if enable && minIns > maxIns {
		minIns, maxIns = maxIns, minIns
	}

	vswitchIDs := cleanStringSlice(npConfig.VSwitchIds)

	// 对于选择“自动创建 VPC”的集群，前端在尚未同步到系统自动生成的 vSwitch 信息时，VSwitchIds 可能为空。
	// 在集群创建时一并创建 VPC 的初始节点池通常不需要显式传入 VSwitchIds，
	// 因此后续新增节点池时，可以回退复用当前集群已有节点池上的 VSwitchIds。
	// 前端也是按相同逻辑处理的。
	if len(vswitchIDs) == 0 {
		nodePoolsInfo, err := DescribeClusterNodePools(client, configSpec)
		if err != nil {
			logrus.Warnf("failed to describe ACK cluster node pools for fallback vswitch_ids: %v", err)
		} else if nodePoolsInfo != nil {
			for _, np := range nodePoolsInfo.Nodepools {
				if np == nil || np.ScalingGroup == nil {
					continue
				}

				candidate := cleanTeaStringSlice(np.ScalingGroup.VswitchIds)
				if len(candidate) > 0 {
					vswitchIDs = candidate
					logrus.Infof("use existing nodepool vswitch_ids for new nodepool [%s]: %v", npConfig.Name, vswitchIDs)
					break
				}
			}
		}
	}

	scalingGroup := &ackapi.CreateClusterNodePoolRequestScalingGroup{
		AutoRenew:          tea.Bool(npConfig.AutoRenew),
		AutoRenewPeriod:    tea.Int64(npConfig.AutoRenewPeriod),
		DataDisks:          dataDiskList,
		InstanceChargeType: tea.String(npConfig.InstanceChargeType),
		InstanceTypes:      tea.StringSlice(npConfig.InstanceTypes),
		KeyPair:            tea.String(npConfig.KeyPair),
		Period:             tea.Int64(npConfig.Period),
		PeriodUnit:         tea.String(npConfig.PeriodUnit),
		ImageType:          tea.String(npConfig.Platform),
		SystemDiskCategory: tea.String(npConfig.SystemDiskCategory),
		SystemDiskSize:     tea.Int64(npConfig.SystemDiskSize),
		VswitchIds:         tea.StringSlice(vswitchIDs),
	}

	if !enable {
		scalingGroup.DesiredSize = tea.Int64(npConfig.InstancesNum)
	}

	return &ackapi.CreateClusterNodePoolRequest{
		AutoScaling: &ackapi.CreateClusterNodePoolRequestAutoScaling{
			Enable:       tea.Bool(enable),
			MinInstances: tea.Int64(minIns),
			MaxInstances: tea.Int64(maxIns),
			Type:         tea.String(scalingType),
		},
		NodepoolInfo: &ackapi.CreateClusterNodePoolRequestNodepoolInfo{
			Name: tea.String(npConfig.Name),
		},
		KubernetesConfig: &ackapi.CreateClusterNodePoolRequestKubernetesConfig{
			Runtime:        tea.String(npConfig.Runtime),
			RuntimeVersion: tea.String(npConfig.RuntimeVersion),
		},
		ScalingGroup: scalingGroup,
	}
}

func autoscalingEnabled(np ackv1.NodePoolInfo) bool {
	return np.AutoScalingEnabled != nil && *np.AutoScalingEnabled
}

func effectiveMinMax(np ackv1.NodePoolInfo) (min, max int64) {
	switch {
	case np.MinInstances != nil && np.MaxInstances != nil:
		return *np.MinInstances, *np.MaxInstances
	case np.MinInstances != nil:
		v := *np.MinInstances
		return v, v
	case np.MaxInstances != nil:
		v := *np.MaxInstances
		return v, v
	default:
		return 0, 0
	}
}

func autoscalingChanged(upstream, desired ackv1.NodePoolInfo) bool {
	uEnabled := autoscalingEnabled(upstream)
	dEnabled := autoscalingEnabled(desired)
	if uEnabled != dEnabled {
		return true
	}
	if !dEnabled {
		return false
	}
	if desired.MinInstances != nil {
		if upstream.MinInstances == nil || *upstream.MinInstances != *desired.MinInstances {
			return true
		}
	}
	if desired.MaxInstances != nil {
		if upstream.MaxInstances == nil || *upstream.MaxInstances != *desired.MaxInstances {
			return true
		}
	}

	return false
}

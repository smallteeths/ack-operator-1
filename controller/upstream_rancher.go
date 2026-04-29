package controller

import (
	"fmt"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	wranglerv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
)

func BuildUpstreamClusterState(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) (*ackv1.ACKClusterConfigSpec, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	cluster, err := ack.DescribeACKCluster(secretsCache, configSpec)
	if err != nil {
		return nil, fmt.Errorf("describe ACK cluster %s failed: %w", configSpec.ClusterID, err)
	}
	if cluster == nil {
		logrus.Warn("BuildUpstreamClusterState: DescribeACKCluster returned nil cluster, using existing spec as upstream state")
		return configSpec.DeepCopy(), nil
	}
	pauseClusterUpgrade, clusterIsUpgrading, err := ack.GetClusterUpgradeFlags(secretsCache, configSpec)
	if err != nil {
		return nil, err
	}
	newSpec := configSpec.DeepCopy()

	newSpec.Name = tea.StringValue(cluster.Name)
	newSpec.ClusterID = tea.StringValue(cluster.ClusterId)
	newSpec.ClusterType = tea.StringValue(cluster.ClusterType)
	newSpec.KubernetesVersion = tea.StringValue(cluster.CurrentVersion)
	newSpec.RegionID = tea.StringValue(cluster.RegionId)
	newSpec.VpcID = tea.StringValue(cluster.VpcId)
	if len(configSpec.ZoneIDs) > 0 {
		newSpec.ZoneIDs = configSpec.ZoneIDs
	} else {
		newSpec.ZoneIDs = []string{tea.StringValue(cluster.ZoneId)}
	}
	newSpec.PauseClusterUpgrade = pauseClusterUpgrade
	newSpec.ClusterIsUpgrading = clusterIsUpgrading
	newSpec.DeletionProtection = tea.BoolValue(cluster.DeletionProtection)
	newSpec.NodePoolList, err = GetNodePoolConfigInfo(secretsCache, configSpec)
	if err != nil {
		return nil, err
	}

	return newSpec, nil
}

func GetNodePoolConfigInfo(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) ([]ackv1.NodePoolInfo, error) {
	nodePoolInfo, err := ack.DescribeACKClusterNodePools(secretsCache, configSpec)
	if err != nil {
		return nil, fmt.Errorf("describe ACK cluster nodepools for cluster %s failed: %w", configSpec.ClusterID, err)
	}

	return ack.ToNodePoolConfigInfo(nodePoolInfo)
}

func GetUserConfig(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterUserKubeconfigResponseBody, error) {
	return ack.DescribeClusterUserKubeconfig(secretsCache, configSpec)
}

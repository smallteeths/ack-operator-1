package controller

import (
	"fmt"
	"strconv"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"github.com/sirupsen/logrus"
)

// importCluster returns an active cluster spec containing the given config's clusterName and region/zone
// and creates a Secret containing the cluster's CA and endpoint retrieved from the cluster object.
func (h *Handler) importCluster(config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	cluster, err := ack.DescribeACKCluster(h.secretsCache, &config.Spec)
	if err != nil {
		return config, err
	}
	if cluster == nil || cluster.State == nil || *cluster.State != ack.ClusterStatusRunning {
		state := "unknown"
		if cluster != nil && cluster.State != nil {
			state = tea.StringValue(cluster.State)
		}

		return config, fmt.Errorf(
			"the current cluster status is %s. Please wait for the cluster to become %s",
			state, ack.ClusterStatusRunning)
	}
	configUpdate := config.DeepCopy()
	fixedSpec := fixConfig(&configUpdate.Spec, cluster)
	if fixedSpec == nil {
		return config, fmt.Errorf("import cluster error: failed to convert and fix the configuration")
	}
	configUpdate.Spec = *fixedSpec
	configUpdate.Spec.NodePoolList, err = GetNodePoolConfigInfo(h.secretsCache, &configUpdate.Spec)
	if err != nil {
		return config, err
	}
	configUpdate, err = h.ackCC.Update(configUpdate)
	if err != nil {
		return config, err
	}
	configStatus := configUpdate.DeepCopy()
	if err = h.createCASecret(configStatus, cluster); err != nil {
		return configStatus, err
	}
	configStatus.Status.Phase = ackConfigActivePhase

	return h.ackCC.UpdateStatus(configStatus)
}

// fixConfig updates the given configSpec in-place based on the upstream cluster detail.
func fixConfig(configSpec *ackv1.ACKClusterConfigSpec, clusterDetail *ackapi.DescribeClusterDetailResponseBody) *ackv1.ACKClusterConfigSpec {
	if clusterDetail == nil || configSpec == nil {
		return configSpec
	}
	// cluster type
	if clusterDetail.ClusterType != nil {
		configSpec.ClusterType = tea.StringValue(clusterDetail.ClusterType)
	}
	// k8s version
	if configSpec.KubernetesVersion == "" && clusterDetail.CurrentVersion != nil {
		configSpec.KubernetesVersion = tea.StringValue(clusterDetail.CurrentVersion)
	}
	// cluster spec
	if clusterDetail.ClusterSpec != nil {
		configSpec.ClusterSpec = tea.StringValue(clusterDetail.ClusterSpec)
	}
	// name
	if clusterDetail.Name != nil {
		configSpec.Name = tea.StringValue(clusterDetail.Name)
	}
	// vSwitchIds
	if len(clusterDetail.VswitchIds) > 0 {
		configSpec.VswitchIds = ack.StringPtrSliceToStringSliceRaw(clusterDetail.VswitchIds)
	} else if clusterDetail.VswitchId != nil {
		configSpec.VswitchIds = ack.SplitNonEmpty(tea.StringValue(clusterDetail.VswitchId))
	}
	// resource group
	if clusterDetail.ResourceGroupId != nil {
		configSpec.ResourceGroupID = tea.StringValue(clusterDetail.ResourceGroupId)
	}
	// CIDR
	if clusterDetail.ContainerCidr != nil {
		configSpec.ContainerCidr = tea.StringValue(clusterDetail.ContainerCidr)
	}
	if clusterDetail.ServiceCidr != nil {
		configSpec.ServiceCidr = tea.StringValue(clusterDetail.ServiceCidr)
	}
	// VPC
	if clusterDetail.VpcId != nil {
		configSpec.VpcID = tea.StringValue(clusterDetail.VpcId)
	}
	// node cidr mask
	if clusterDetail.NodeCidrMask != nil {
		maskNum, err := strconv.Atoi(tea.StringValue(clusterDetail.NodeCidrMask))
		if err != nil {
			logrus.Warnf("get node-cidr-mask failed: %s", tea.StringValue(clusterDetail.NodeCidrMask))
		} else {
			configSpec.NodeCidrMask = int64(maskNum)
		}
	}
	// proxy mode
	if clusterDetail.ProxyMode != nil {
		configSpec.ProxyMode = tea.StringValue(clusterDetail.ProxyMode)
	}

	return configSpec
}

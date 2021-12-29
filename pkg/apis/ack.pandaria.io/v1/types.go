/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ACKClusterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ACKClusterConfigSpec   `json:"spec"`
	Status ACKClusterConfigStatus `json:"status"`
}

// ACKClusterConfigSpec is the spec for a ACKClusterConfig resource
type ACKClusterConfigSpec struct {
	Name                     string         `json:"name,omitempty"`
	ClusterID                string         `json:"cluster_id,omitempty" norman:"noupdate"`
	AliyunCredentialSecret   string         `json:"aliyun_credential_secret,omitempty"`
	DisableRollback          bool           `json:"disableRollback"`
	ClusterType              string         `json:"clusterType,omitempty" norman:"noupdate"`
	KubernetesVersion        string         `json:"kubernetesVersion,omitempty"`
	TimeoutMins              int64          `json:"timeoutMins,omitempty" norman:"noupdate"`
	RegionID                 string         `json:"regionId,omitempty" norman:"noupdate"`
	VpcID                    string         `json:"vpcId,omitempty" norman:"noupdate"`
	ZoneID                   string         `json:"zoneId,omitempty" norman:"noupdate"`
	ContainerCidr            string         `json:"containerCidr,omitempty" norman:"noupdate"`
	ServiceCidr              string         `json:"serviceCidr,omitempty" norman:"noupdate"`
	NodeCidrMask             int64          `json:"nodeCidrMask,omitempty" norman:"noupdate"`
	CloudMonitorFlags        bool           `json:"cloudMonitorFlags"`
	LoginPassword            string         `json:"loginPassword,omitempty"`
	KeyPair                  string         `json:"keyPair,omitempty" norman:"noupdate"`
	MasterCount              int64          `json:"masterCount,omitempty" norman:"noupdate"`
	MasterVswitchIds         []string       `json:"masterVswitchIds,omitempty" norman:"noupdate"`
	MasterInstanceTypes      []string       `json:"masterInstanceTypes,omitempty" norman:"noupdate"`
	MasterInstanceChargeType string         `json:"masterInstanceChargeType,omitempty" norman:"noupdate"`
	MasterPeriod             int64          `json:"masterPeriod,omitempty" norman:"noupdate"`
	MasterPeriodUnit         string         `json:"masterPeriodUnit,omitempty" norman:"noupdate"`
	MasterAutoRenew          bool           `json:"masterAutoRenew" norman:"noupdate"`
	MasterAutoRenewPeriod    int64          `json:"masterAutoRenewPeriod,omitempty" norman:"noupdate"`
	MasterSystemDiskCategory string         `json:"masterSystemDiskCategory,omitempty" norman:"noupdate"`
	MasterSystemDiskSize     int64          `json:"masterSystemDiskSize,omitempty" norman:"noupdate"`
	VswitchIds               []string       `json:"vswitchIds,omitempty" norman:"noupdate"`
	SnatEntry                bool           `json:"snatEntry"`
	ProxyMode                string         `json:"proxyMode,omitempty"`
	EndpointPublicAccess     bool           `json:"endpointPublicAccess"`
	SecurityGroupID          string         `json:"securityGroupId,omitempty" norman:"noupdate"`
	SSHFlags                 bool           `json:"sshFlags"`
	OsType                   string         `json:"osType,omitempty"`
	Platform                 string         `json:"platform,omitempty"`
	ResourceGroupID          string         `json:"resourceGroupId,omitempty" norman:"noupdate"`
	Imported                 bool           `json:"imported" norman:"noupdate"`
	NodePoolList             []NodePoolInfo `json:"node_pool_list,omitempty"`
}

type ACKClusterConfigStatus struct {
	Phase          string `json:"phase"`
	FailureMessage string `json:"failureMessage"`
}

type DiskInfo struct {
	Category             string `json:"category"`
	Size                 int64  `json:"size"`
	Encrypted            string `json:"encrypted"`
	AutoSnapshotPolicyID string `json:"auto_snapshot_policy_id"`
}

type NodePoolInfo struct {
	/* node_pool_info */
	NodepoolId string `json:"nodepool_id,omitempty"`
	Name       string `json:"name,omitempty"`
	/* auto_scaling */
	InstancesNum          int64  `json:"instances_num,omitempty"`
	ScalingType           string `json:"scaling_type,omitempty"`
	IsBondEip             bool   `json:"is_bond_eip,omitempty" norman:"noupdate"`
	EipInternetChargeType string `json:"eip_internet_charge_type,omitempty" norman:"noupdate"`
	EipBandwidth          int64  `json:"eip_bandwidth,omitempty" norman:"noupdate"`
	/* scaling_group */
	AutoRenew          bool       `json:"auto_renew,omitempty" norman:"noupdate"`
	AutoRenewPeriod    int64      `json:"auto_renew_period,omitempty" norman:"noupdate"`
	DataDisk           []DiskInfo `json:"data_disk,omitempty"`
	InstanceChargeType string     `json:"instance_charge_type,omitempty" norman:"noupdate"`
	InstanceTypes      []string   `json:"instance_types,omitempty"`
	KeyPair            string     `json:"key_pair,omitempty"`
	LoginPassword      string     `json:"login_password,omitempty"`
	Period             int64      `json:"period,omitempty" norman:"noupdate"`
	PeriodUnit         string     `json:"period_unit,omitempty" norman:"noupdate"`
	Platform           string     `json:"platform,omitempty"`
	SystemDiskCategory string     `json:"system_disk_category,omitempty"`
	SystemDiskSize     int64      `json:"system_disk_size,omitempty"`
	VSwitchIds         []string   `json:"v_switch_ids,omitempty"`
}

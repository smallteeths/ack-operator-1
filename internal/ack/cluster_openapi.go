package ack

import (
	"fmt"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"github.com/cnrancher/ack-operator/utils"
	wranglerv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// V2 ACK client
func NewACKClient(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.Client, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("error get aliyunCredentialSecret configSpec is nil")
	}
	if configSpec.AliyunCredentialSecret == "" {
		return nil, fmt.Errorf("error while getting aliyunCredentialSecret: empty secret reference")
	}
	ns, id := utils.Parse(configSpec.AliyunCredentialSecret)
	secret, err := secretsCache.Get(ns, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get aliyun credential secret %s/%s: %w", ns, id, err)
	}
	accessKeyBytes := secret.Data["aliyunecscredentialConfig-accessKeyId"]
	secretKeyBytes := secret.Data["aliyunecscredentialConfig-accessKeySecret"]
	if accessKeyBytes == nil || secretKeyBytes == nil {
		return nil, fmt.Errorf("invalid aliyun cloud credential: accessKeyId or accessKeySecret missing")
	}
	accessKeyId := string(accessKeyBytes)
	accessKeySecret := string(secretKeyBytes)
	// 构造 Credential
	credConfig := &credential.Config{
		Type:            tea.String("access_key"),
		AccessKeyId:     tea.String(accessKeyId),
		AccessKeySecret: tea.String(accessKeySecret),
		// to do STS，可再加 SecurityToken: tea.String(token)
	}
	cred, err := credential.NewCredential(credConfig)
	if err != nil {
		return nil, fmt.Errorf("create aliyun credential failed: %w", err)
	}
	cfg := &openapi.Config{
		Credential: cred,
		RegionId:   tea.String(configSpec.RegionID),
	}
	cfg.Endpoint = tea.String("cs." + configSpec.RegionID + ".aliyuncs.com")
	client, err := ackapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create cs20151215 client failed: %w", err)
	}

	return client, nil
}

// V2 Describe ACK Cluster
func DescribeACKCluster(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterDetailResponseBody, error) {
	client, err := NewACKClient(secretsCache, configSpec)
	if err != nil {
		return nil, err
	}

	return DescribeCluster(client, configSpec)
}

// V2 Describe ACK Cluster Node pool
func DescribeACKClusterNodePools(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterNodePoolsResponseBody, error) {
	client, err := NewACKClient(secretsCache, configSpec)
	if err != nil {
		return nil, err
	}
	return DescribeClusterNodePools(client, configSpec)
}

func CreateACK(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) error {
	if err := validateCreateRequest(configSpec); err != nil {
		return err
	}
	req := newClusterCreateRequest(configSpec)
	runtime := &util.RuntimeOptions{}
	headers := make(map[string]*string)
	resp, err := client.CreateClusterWithOptions(req, headers, runtime)
	if err != nil {
		return fmt.Errorf("create ACK cluster failed: %w", err)
	}
	if resp.Body == nil || resp.Body.ClusterId == nil {
		return fmt.Errorf("create ACK cluster succeeded but response clusterId is nil")
	}
	configSpec.ClusterID = tea.StringValue(resp.Body.ClusterId)

	return nil
}

func DescribeCluster(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterDetailResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("ACK configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	headers := make(map[string]*string)
	runtime := &util.RuntimeOptions{}
	resp, err := client.DescribeClusterDetailWithOptions(tea.String(configSpec.ClusterID), headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("describe ACK cluster failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("describe ACK cluster succeeded but response body is nil")
	}
	return resp.Body, nil
}

func DescribeACKTaskInfo(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeTaskInfoResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("ACK configSpec is nil")
	}
	if configSpec.TaskId == "" {
		return nil, fmt.Errorf("taskId is empty")
	}
	headers := make(map[string]*string)
	runtime := &util.RuntimeOptions{}
	resp, err := client.DescribeTaskInfoWithOptions(&configSpec.TaskId, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("describe ACK task info failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("describe ACK task info succeeded but response body is nil")
	}
	return resp.Body, nil
}

func UpgradeACKCluster(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.UpgradeClusterResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("upstreamSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	if configSpec.KubernetesVersion == "" {
		return nil, fmt.Errorf("KubernetesVersion is empty")
	}

	req := &ackapi.UpgradeClusterRequest{
		NextVersion: tea.String(configSpec.KubernetesVersion),
	}
	headers := make(map[string]*string)
	runtime := &util.RuntimeOptions{}
	resp, err := client.UpgradeClusterWithOptions(tea.String(configSpec.ClusterID), req, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("upgrade ACK cluster failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("upgrade ACK cluster succeeded but response body is nil")
	}
	return resp.Body, nil
}

func ModifyACKCluster(
	client *ackapi.Client,
	currentSpec *ackv1.ACKClusterConfigSpec,
	desiredSpec *ackv1.ACKClusterConfigSpec,
) (*ackapi.ModifyClusterResponseBody, bool, error) {
	if desiredSpec == nil {
		return nil, false, fmt.Errorf("upstreamSpec is nil")
	}
	if currentSpec == nil {
		return nil, false, fmt.Errorf("currentSpec is nil")
	}
	if desiredSpec.ClusterID == "" {
		return nil, false, fmt.Errorf("clusterID is empty")
	}
	req := &ackapi.ModifyClusterRequest{}
	changed := false
	if currentSpec.Name != desiredSpec.Name {
		if desiredSpec.Name == "" {
			return nil, false, fmt.Errorf("ACK name is empty")
		}

		req.ClusterName = tea.String(desiredSpec.Name)
		changed = true
	}
	if currentSpec.DeletionProtection != desiredSpec.DeletionProtection {
		req.DeletionProtection = tea.Bool(desiredSpec.DeletionProtection)
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	headers := make(map[string]*string)
	runtime := &util.RuntimeOptions{}

	resp, err := client.ModifyClusterWithOptions(tea.String(desiredSpec.ClusterID), req, headers, runtime)
	if err != nil {
		return nil, false, fmt.Errorf("modify ACK cluster failed: %w", err)
	}
	if resp.Body == nil {
		return nil, false, fmt.Errorf("modify ACK cluster succeeded but response body is nil")
	}

	return resp.Body, true, nil
}

func DescribeClusterNodePools(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterNodePoolsResponseBody, error) {
	if configSpec == nil {
		return nil, fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	req := &ackapi.DescribeClusterNodePoolsRequest{}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.DescribeClusterNodePoolsWithOptions(tea.String(configSpec.ClusterID), req, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("describe ACK cluster nodepools failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("describe ACK cluster nodepools succeeded but response body is nil")
	}

	return resp.Body, nil
}

func RemoveACKCluster(client *ackapi.Client, configSpec *ackv1.ACKClusterConfigSpec) error {
	if configSpec == nil {
		return fmt.Errorf("configSpec is nil")
	}
	if configSpec.ClusterID == "" {
		return fmt.Errorf("clusterID is empty")
	}

	return wait.ExponentialBackoff(backoff, func() (bool, error) {
		req := &ackapi.DeleteClusterRequest{}
		headers := map[string]*string{}
		runtime := &util.RuntimeOptions{}
		_, err := client.DeleteClusterWithOptions(tea.String(configSpec.ClusterID), req, headers, runtime)
		if err != nil {
			return false, err
		}

		return true, nil
	})
}

func GetClusterUpgradeFlags(secretsCache wranglerv1.SecretCache, spec *ackv1.ACKClusterConfigSpec) (pause bool, upgrading bool, err error) {
	if spec.ClusterID == "" || spec.TaskId == "" {
		return false, false, nil
	}
	client, err := NewACKClient(secretsCache, spec)
	if err != nil {
		return false, false, err
	}
	taskInfo, err := DescribeACKTaskInfo(client, spec)
	if err != nil {
		return false, false, fmt.Errorf("failed to describe task info for cluster %s: %w", spec.ClusterID, err)
	}
	if taskInfo.State == nil {
		return false, false, nil
	}

	switch *taskInfo.State {
	case UpdateK8sRunningStatus:
		return false, true, nil
	case UpdateK8sFailStatus:
		return true, false, nil
	default:
		return false, false, nil
	}
}

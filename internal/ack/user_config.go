package ack

import (
	"fmt"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	wranglerv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
)

// GetUserConfig returns user config
func DescribeClusterUserKubeconfig(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterUserKubeconfigResponseBody, error) {
	client, err := NewACKClient(secretsCache, configSpec)
	if err != nil {
		return nil, err
	}
	if configSpec == nil {
		return nil, fmt.Errorf("state is nil")
	}
	if configSpec.ClusterID == "" {
		return nil, fmt.Errorf("clusterID is empty")
	}
	req := &ackapi.DescribeClusterUserKubeconfigRequest{}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.DescribeClusterUserKubeconfigWithOptions(tea.String(configSpec.ClusterID), req, headers, runtime)
	if err != nil {
		return nil, fmt.Errorf("describe ACK cluster user kubeconfig failed: %w", err)
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("describe ACK cluster user kubeconfig succeeded but response body is nil")
	}

	return resp.Body, nil
}

package ack

import (
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"

	ackapi "github.com/alibabacloud-go/cs-20151215/v2/client"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

// GetUserConfig returns user config
func GetUserConfig(svc *sdk.Client, state *ackv1.ACKClusterConfigSpec) (*ackapi.DescribeClusterUserKubeconfigResponseBody, error) {
	request := requests.NewCommonRequest()
	request.Method = "GET"
	request.Scheme = "https"
	request.Domain = "cs." + state.RegionID + ".aliyuncs.com"
	request.Version = DefaultACKAPIVersion
	request.PathPattern = "/k8s/" + state.ClusterID + "/user_config"
	request.Headers["Content-Type"] = "application/json"

	body := `{}`
	request.Content = []byte(body)

	kubeConfig := &ackapi.DescribeClusterUserKubeconfigResponseBody{}
	err := ProcessRequest(svc, request, kubeConfig)
	if err != nil {
		return nil, err
	}
	return kubeConfig, nil
}

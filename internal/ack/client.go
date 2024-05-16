package ack

import (
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	sts "github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
)

func GetACKClient(regionID, accessKeyID, accessKeySecret string) (*sdk.Client, error) {
	return sdk.NewClientWithAccessKey(regionID, accessKeyID, accessKeySecret)
}

func GetACKSTSClient(regionID, accessKeyID, accessKeySecret string) (*sts.Client, error) {
	config := sdk.NewConfig()
	credential := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
	return sts.NewClientWithOptions(regionID, config, credential)
}

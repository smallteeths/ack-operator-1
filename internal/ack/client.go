package ack

import (
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
)

func GetACKClient(regionID, accessKeyID, accessKeySecret, token string) (*sdk.Client, error) {
	if token != "" {
		return sdk.NewClientWithStsToken(regionID, accessKeyID, accessKeySecret, token)
	}
	return sdk.NewClientWithAccessKey(regionID, accessKeyID, accessKeySecret)
}

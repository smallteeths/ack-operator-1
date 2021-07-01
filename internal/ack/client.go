package ack

import (
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
)

func GetACKClient(regionID, accessKeyID, accessKeySecret string) (*sdk.Client, error) {
	return sdk.NewClientWithAccessKey(regionID, accessKeyID, accessKeySecret)
}

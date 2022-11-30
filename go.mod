module github.com/cnrancher/ack-operator

go 1.16

replace k8s.io/client-go => k8s.io/client-go v0.18.0

require (
	github.com/alibabacloud-go/cs-20151215/v3 v3.0.22
	github.com/alibabacloud-go/tea v1.1.20
	github.com/alibabacloud-go/tea-utils v1.4.3 // indirect
	github.com/aliyun/alibaba-cloud-sdk-go v1.62.11
	github.com/google/uuid v1.1.1
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/rancher/lasso v0.0.0-20210616224652-fc3ebd901c08
	github.com/rancher/wrangler v0.8.3
	github.com/rancher/wrangler-api v0.6.1-0.20200427172631-a7c2f09b783e
	github.com/sirupsen/logrus v1.4.2
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
)

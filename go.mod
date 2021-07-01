module github.com/cnrancher/ack-operator

go 1.16

replace k8s.io/client-go => k8s.io/client-go v0.18.0

require (
	github.com/alibabacloud-go/cs-20151215/v2 v2.4.1
	github.com/alibabacloud-go/darabonba-openapi v0.1.5 // indirect
	github.com/alibabacloud-go/tea v1.1.15
	github.com/aliyun/alibaba-cloud-sdk-go v1.61.1099
	github.com/google/uuid v1.1.1
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/rancher/lasso v0.0.0-20200905045615-7fcb07d6a20b
	github.com/rancher/wrangler v0.7.3-0.20201020003736-e86bc912dfac
	github.com/rancher/wrangler-api v0.6.1-0.20200427172631-a7c2f09b783e
	github.com/sirupsen/logrus v1.4.2
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/klog v1.0.0 // indirect
)

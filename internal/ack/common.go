package ack

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	DefaultACKAPIVersion = "2015-12-15"
	DefaultNodePoolName  = "default-nodepool"
)

// Status indicates how to handle the response from a request to update a resource
type Status int

// Status indicators
const (
	// Changed means the request to change resource was accepted and change is in progress
	Changed Status = iota
	// Retry means the request to change resource was rejected due to an expected error and should be retried later
	Retry
	// NotChanged means the resource was not changed, either due to error or because it was unnecessary
	NotChanged
)

// State of task
const (
	TaskStatusSuccess = "success"
	TaskStatusRunning = "running"
	TaskStatusFailed  = "failed"
)

// State of instance
const (
	ClusterStatusRunning  = "running"
	ClusterStatusError    = "failed"
	ClusterStatusUpdating = "updating"
	ClusterStatusScaling  = "scaling"
	ClusterStatusRemoving = "removing"
)

// State of node pool
const (
	NodePoolStatusActive   = "active"
	NodePoolStatusInitial  = "initial"
	NodePoolStatusScaling  = "scaling"
	NodePoolStatusRemoving = "removing"
	NodePoolStatusDeleting = "deleting"
	NodePoolStatusUpdating = "updating"
)

const (
	waitSec      = 30
	backoffSteps = 12
)

var backoff = wait.Backoff{
	Duration: waitSec * time.Second,
	Steps:    backoffSteps,
}

type clusterCreateResponse struct {
	ClusterID string `json:"cluster_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}

func ProcessRequest(svc *sdk.Client, request *requests.CommonRequest, obj interface{}) error {
	response, err := svc.ProcessCommonRequest(request)
	if err != nil && !strings.Contains(err.Error(), "JsonUnmarshalError") {
		return err
	}
	if obj != nil {
		if err := json.Unmarshal(response.GetHttpContentBytes(), obj); err != nil {
			return err
		}
	}
	return nil
}

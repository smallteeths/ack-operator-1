package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	ackapi "github.com/alibabacloud-go/cs-20151215/v7/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	v12 "github.com/cnrancher/ack-operator/pkg/generated/controllers/ack.pandaria.io/v1"
	wranglerv1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

const (
	ACKClusterConfigKind     = "ACKClusterConfig"
	controllerName           = "ack-controller"
	controllerRemoveName     = "ack-controller-remove"
	ackConfigCreatingPhase   = "creating"
	ackConfigNotCreatedPhase = ""
	ackConfigActivePhase     = "active"
	ackConfigUpdatingPhase   = "updating"
	ackConfigImportingPhase  = "importing"
	wait                     = 30
)

type Handler struct {
	ackCC           v12.ACKClusterConfigClient
	ackEnqueueAfter func(namespace, name string, duration time.Duration)
	ackEnqueue      func(namespace, name string)
	secrets         wranglerv1.SecretClient
	secretsCache    wranglerv1.SecretCache
}

func Register(
	ctx context.Context,
	secrets wranglerv1.SecretController,
	ack v12.ACKClusterConfigController) {

	controller := &Handler{
		ackCC:           ack,
		ackEnqueue:      ack.Enqueue,
		ackEnqueueAfter: ack.EnqueueAfter,
		secretsCache:    secrets.Cache(),
		secrets:         secrets,
	}

	// Register handlers
	ack.OnChange(ctx, controllerName, controller.recordError(controller.OnAckConfigChanged))
	ack.OnRemove(ctx, controllerRemoveName, controller.recordError(controller.OnAckConfigRemoved))
}

func (h *Handler) OnAckConfigChanged(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	if config == nil {
		return nil, nil
	}
	if config.DeletionTimestamp != nil {
		return nil, nil
	}

	switch config.Status.Phase {
	case ackConfigImportingPhase:
		return h.importCluster(config)
	case ackConfigNotCreatedPhase:
		return h.create(config)
	case ackConfigCreatingPhase:
		return h.waitForCreationComplete(config)
	case ackConfigActivePhase, ackConfigUpdatingPhase:
		return h.checkAndUpdate(config)
	}

	return config, nil
}

// recordError writes the error return by onChange to the failureMessage field on status. If there is no error, then
// empty string will be written to status
func (h *Handler) recordError(onChange func(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error)) func(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	return func(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
		var err error
		var message string
		config, err = onChange(key, config)
		if config == nil {
			// ACK config is likely deleting
			return config, err
		}
		if err != nil {
			message = err.Error()
		}

		if config.Status.FailureMessage == message {
			return config, err
		}

		config = config.DeepCopy()

		if message != "" {
			if config.DeletionTimestamp == nil && config.Status.Phase == ackConfigActivePhase {
				// can assume an update is failing
				config.Status.Phase = ackConfigUpdatingPhase
			}
		}
		config.Status.FailureMessage = message

		var recordErr error
		config, recordErr = h.ackCC.UpdateStatus(config)
		if recordErr != nil {
			logrus.Errorf("Error recording ackcc [%s] failure message: %s", config.Spec.Name, recordErr.Error())
		}
		return config, err
	}
}

func (h *Handler) checkAndUpdate(config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	cfg := config.DeepCopy()

	cluster, err := ack.DescribeACKCluster(h.secretsCache, &cfg.Spec)
	if err != nil {
		return cfg, err
	}
	if cluster == nil {
		return cfg, fmt.Errorf("update cluster error: the cluster is nil, indicating no cluster information is available")
	}
	var clusterState string

	if cluster.State != nil {
		clusterState = *cluster.State
	} else {
		logrus.Warnf("ACK cluster [%s] state is nil", cfg.Name)
	}
	logrus.Infof("ackconfig cluster refresh updating %s", cfg.Name)
	var (
		clusterIsUpgrading bool
		client             *ackapi.Client
	)

	getClient := func() (*ackapi.Client, error) {
		if client != nil {
			return client, nil
		}
		c, err := ack.NewACKClient(h.secretsCache, &cfg.Spec)
		if err != nil {
			return nil, err
		}
		client = c

		return client, nil
	}

	// 检查 ACK 的升级任务状态
	if cfg.Spec.ClusterID != "" && cfg.Spec.TaskId != "" {
		client, err := getClient()
		if err != nil {
			return cfg, err
		}

		taskInfo, err := ack.DescribeACKTaskInfo(client, &cfg.Spec)
		if err != nil {
			return cfg, fmt.Errorf("failed to describe task info for cluster %s: %w", cfg.Spec.ClusterID, err)
		}

		if taskInfo.State != nil {
			switch *taskInfo.State {
			case ack.UpdateK8sRunningStatus:
				clusterIsUpgrading = true
			case ack.UpdateK8sFailStatus:
				if taskInfo.Error == nil || taskInfo.Error.Message == nil {
					return cfg, fmt.Errorf("update cluster %s failed: error message is missing", cfg.Spec.ClusterID)
				}
				errMsg := fmt.Sprintf(`{"%s":"%s"}`, ack.UpdateK8SError, *taskInfo.Error.Message)
				return cfg, fmt.Errorf("update cluster %s failed: %s", cfg.Spec.ClusterID, errMsg)
			case ack.UpdateK8sSuccessStatus:
				cfg = cfg.DeepCopy()
				cfg.Spec.TaskId = ""
				return h.ackCC.Update(cfg)
			}
		}
	}

	// 如果版本有变更则触发 ACK k8s 版本升级
	if !clusterIsUpgrading &&
		!cfg.Spec.PauseClusterUpgrade &&
		!cfg.Spec.ClusterIsUpgrading &&
		(cfg.Status.Phase == ackConfigActivePhase || strings.Contains(cfg.Status.FailureMessage, ack.UpdateK8SVersionApiError)) {

		if cluster.CurrentVersion != nil && cfg.Spec.KubernetesVersion != *cluster.CurrentVersion && !cfg.Spec.Imported {
			cfg.Status.Phase = ackConfigUpdatingPhase
			client, err := getClient()
			if err != nil {
				return cfg, err
			}
			upgradeClusterResponse, err := ack.UpgradeACKCluster(client, &cfg.Spec)
			if err != nil {
				updateErr := fmt.Errorf(`{"%s":"%s"}`, ack.UpdateK8SVersionApiError, err.Error())

				return cfg, updateErr
			}
			if upgradeClusterResponse.TaskId == nil {
				return cfg, fmt.Errorf("upgrade ACK cluster succeeded but taskId is nil")
			}

			cfg.Spec.TaskId = *upgradeClusterResponse.TaskId
			return h.ackCC.Update(cfg)
		}
	}

	// 集群级别处于变更中，需要等待集群变为 active
	if clusterState == ack.ClusterStatusUpdating ||
		clusterState == ack.ClusterStatusScaling ||
		clusterState == ack.ClusterStatusRemoving ||
		clusterIsUpgrading {
		logrus.Infof("waiting for cluster [%s] to finish %s", cfg.Name, clusterState)
		if cfg.Status.Phase != ackConfigUpdatingPhase {
			cfg = cfg.DeepCopy()
			cfg.Status.Phase = ackConfigUpdatingPhase
			return h.ackCC.UpdateStatus(cfg)
		}
		h.ackEnqueueAfter(cfg.Namespace, cfg.Name, 30*time.Second)
		return cfg, nil
	}

	// 对应 ACK NodePool 的状态需要等待节点都创建完成
	nodePoolsInfo, err := ack.DescribeACKClusterNodePools(h.secretsCache, &cfg.Spec)
	if err != nil {
		return cfg, err
	}
	// 如果 ACK NodePool 没有节点则判断 ack 还在 updating 状态
	if nodePoolsInfo == nil || len(nodePoolsInfo.Nodepools) == 0 {
		if cfg.Status.Phase != ackConfigUpdatingPhase {
			cfg = cfg.DeepCopy()
			cfg.Status.Phase = ackConfigUpdatingPhase
			cfg, err = h.ackCC.UpdateStatus(cfg)
			if err != nil {
				return cfg, err
			}
		}

		logrus.Infof("waiting for cluster [%s] to update node pools: no nodepool information available yet", cfg.Name)
		h.ackEnqueueAfter(cfg.Namespace, cfg.Name, 30*time.Second)
		return cfg, nil
	}
	for _, np := range nodePoolsInfo.Nodepools {
		if np == nil {
			logrus.Warn("Warning update cluster: The nodepool is nil, indicating no nodepool information is available")
			continue
		}
		if np.Status == nil || np.Status.State == nil {
			logrus.Warn("Warning update cluster: The nodepool status is nil, indicating no nodepool information is available")
			continue
		}

		status := *np.Status.State
		if status == ack.NodePoolStatusScaling ||
			status == ack.NodePoolStatusDeleting ||
			status == ack.NodePoolStatusInitial ||
			status == ack.NodePoolStatusUpdating ||
			status == ack.NodePoolStatusRemoving {
			if cfg.Status.Phase != ackConfigUpdatingPhase {
				cfg = cfg.DeepCopy()
				cfg.Status.Phase = ackConfigUpdatingPhase
				cfg, err = h.ackCC.UpdateStatus(cfg)
				if err != nil {
					return cfg, err
				}
			}
			nodePoolName := "<unknown>"
			if np.NodepoolInfo != nil && np.NodepoolInfo.Name != nil {
				nodePoolName = *np.NodepoolInfo.Name
			}

			logrus.Infof("waiting for cluster [%s] to [%s] node pool [%s]", cfg.Name, status, nodePoolName)
			h.ackEnqueueAfter(cfg.Namespace, cfg.Name, 30*time.Second)
			return cfg, nil
		}
	}

	// 创建完成之后获得当前集群的一些信息
	upstreamSpec, err := BuildUpstreamClusterState(h.secretsCache, &cfg.Spec)
	if err != nil {
		return cfg, err
	}

	return h.updateUpstreamClusterState(cfg, upstreamSpec)
}

// enqueueUpdate enqueues the config if it is already in the updating phase. Otherwise, the
// phase is updated to "updating". This is important because the object needs to reenter the
// onChange handler to start waiting on the update.
func (h *Handler) enqueueUpdate(config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	if config.Status.Phase == ackConfigUpdatingPhase {
		h.ackEnqueue(config.Namespace, config.Name)
		return config, nil
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		config, err = h.ackCC.Get(config.Namespace, config.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		config = config.DeepCopy()
		config.Status.Phase = ackConfigUpdatingPhase
		config, err = h.ackCC.UpdateStatus(config)
		return err
	})
	return config, err
}

// updateUpstreamClusterState sync config to upstream cluster
func (h *Handler) updateUpstreamClusterState(config *ackv1.ACKClusterConfig, upstreamSpec *ackv1.ACKClusterConfigSpec) (*ackv1.ACKClusterConfig, error) {
	changed := ack.NotChanged
	if !config.Spec.Imported {
		client, err := ack.NewACKClient(h.secretsCache, &config.Spec)
		if err != nil {
			return config, err
		}
		if _, clusterChanged, err := ack.ModifyACKCluster(client, &config.Spec, upstreamSpec); err != nil {
			return config, err
		} else if clusterChanged {
			changed = ack.Changed
		}
		nodepoolChanged, err := ack.BatchUpdateClusterNodePools(client, &config.Spec)
		if err != nil {
			return config, err
		}
		if nodepoolChanged == ack.Changed {
			changed = ack.Changed
		}
	}
	if changed == ack.Changed {
		return h.setUpdatingPhase(config)
	}
	if config.Status.Phase != ackConfigActivePhase {
		logrus.Infof("cluster [%s] finished updating", config.Name)
		cfg := config.DeepCopy()
		cfg.Status.Phase = ackConfigActivePhase
		return h.ackCC.UpdateStatus(cfg)
	}

	return config, nil
}

func (h *Handler) setUpdatingPhase(config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	configUpdate := config.DeepCopy()
	configUpdate, err := h.ackCC.Update(configUpdate)
	if err != nil {
		return config, err
	}
	config = configUpdate.DeepCopy()
	config.Status.Phase = ackConfigUpdatingPhase
	return h.enqueueUpdate(config)
}

func (h *Handler) waitForCreationComplete(config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	cluster, err := ack.DescribeACKCluster(h.secretsCache, &config.Spec)
	if err != nil {
		return config, err
	}
	if cluster == nil {
		return config, fmt.Errorf("create cluster error: get the cluster is nil, indicating no cluster information is available")
	}
	if *cluster.State == ack.ClusterStatusError {
		return config, fmt.Errorf("creation failed for cluster %v", config.Spec.Name)
	}
	if *cluster.State == ack.ClusterStatusRunning {
		if err := h.createCASecret(config, cluster); err != nil {
			return config, err
		}
		logrus.Infof("Cluster %v is running", config.Spec.Name)
		config = config.DeepCopy()
		config.Status.Phase = ackConfigActivePhase
		return h.ackCC.UpdateStatus(config)
	}
	logrus.Infof("waiting for cluster [%s] to finish creating", config.Name)
	h.ackEnqueueAfter(config.Namespace, config.Name, wait*time.Second)

	return config, nil
}

// createCASecret creates a secret containing a CA and endpoint for use in generating a kubeconfig file.
func (h *Handler) createCASecret(config *ackv1.ACKClusterConfig, cluster *ackapi.DescribeClusterDetailResponseBody) error {
	client, err := ack.NewACKClient(h.secretsCache, &config.Spec)
	if err != nil {
		return err
	}
	req := &ackapi.DescribeClusterUserKubeconfigRequest{}
	headers := map[string]*string{}
	runtime := &util.RuntimeOptions{}
	resp, err := client.DescribeClusterUserKubeconfigWithOptions(tea.String(config.Spec.ClusterID), req, headers, runtime)
	if err != nil {
		return fmt.Errorf("describe ACK cluster user kubeconfig failed: %w", err)
	}
	if resp.Body == nil || resp.Body.Config == nil {
		return fmt.Errorf("describe ACK cluster user kubeconfig succeeded but config is nil")
	}
	kubeConfig := tea.StringValue(resp.Body.Config)
	// Get kubeconfig from kube-rest-config
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfig))
	if err != nil {
		return err
	}
	_, err = h.secrets.Create(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.Name,
				Namespace: config.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: ackv1.SchemeGroupVersion.String(),
						Kind:       ACKClusterConfigKind,
						UID:        config.UID,
						Name:       config.Name,
					},
				},
			},
			Data: map[string][]byte{
				"endpoint": []byte(restConfig.Host),
				"ca":       []byte(base64.StdEncoding.EncodeToString(restConfig.CAData)),
			},
		})
	if k8serrors.IsAlreadyExists(err) {
		logrus.Debugf("CA secret [%s] already exists, ignoring", config.Name)
		return nil
	}

	return err
}

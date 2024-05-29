package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	ackapi "github.com/alibabacloud-go/cs-20151215/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	v12 "github.com/cnrancher/ack-operator/pkg/generated/controllers/ack.pandaria.io/v1"
	"github.com/cnrancher/ack-operator/store"
	"github.com/cnrancher/ack-operator/utils"
	wranglerv1 "github.com/rancher/wrangler/v2/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	defaultRegion            = "cn-hangzhou"
	defaultAPIScheme         = "https"
)

var (
	secretStore *store.StsTokenStore
	once        sync.Once
)

func initSecretStore() {
	secretStore = store.NewStsTokenStore()
}

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

	once.Do(initSecretStore)
	controller := &Handler{
		ackCC:           ack,
		ackEnqueue:      ack.Enqueue,
		ackEnqueueAfter: ack.EnqueueAfter,
		secretsCache:    secrets.Cache(),
		secrets:         secrets,
	}

	// Register handlers
	ack.OnChange(ctx, controllerName, controller.recordError(controller.OnAckConfigChanged))
	ack.OnRemove(ctx, controllerRemoveName, controller.OnAckConfigRemoved)
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
		return h.importCluster(config, secretStore)
	case ackConfigNotCreatedPhase:
		return h.create(config, secretStore)
	case ackConfigCreatingPhase:
		return h.waitForCreationComplete(config, secretStore)
	case ackConfigActivePhase, ackConfigUpdatingPhase:
		return h.checkAndUpdate(config, secretStore)
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
			if config.Status.Phase == ackConfigActivePhase {
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

// importCluster returns an active cluster spec containing the given config's clusterName and region/zone
// and creates a Secret containing the cluster's CA and endpoint retrieved from the cluster object.
func (h *Handler) importCluster(config *ackv1.ACKClusterConfig, innerSecretStore *store.StsTokenStore) (*ackv1.ACKClusterConfig, error) {
	clusterMap, err := GetClusterWithParam(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}
	if clusterMap == nil {
		return config, fmt.Errorf("get import cluster error: The cluster is nil, indicating no cluster information is available")
	}
	cluster := &ackapi.DescribeClusterDetailResponseBody{}
	err = utils.ConvertMapToObj(*clusterMap, cluster)
	if err != nil {
		return config, err
	}

	configUpdate := config.DeepCopy()
	fixedSpec := FixConfig(&config.Spec, *clusterMap)
	if fixedSpec == nil {
		return config, fmt.Errorf("import cluster error: failed to convert and fix the configuration")
	}
	configUpdate.Spec = *fixedSpec
	configUpdate.Spec.NodePoolList, err = GetNodePoolConfigInfo(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}
	pauseClusterUpgrade := false
	clusterIsUpgrading := false
	if cluster.ClusterId != nil && *cluster.ClusterId != "" {
		client, err := GetClient(h.secretsCache, &configUpdate.Spec, innerSecretStore)
		if err != nil {
			return config, err
		}
		upgradeStatus, err := ack.GetUpgradeStatus(client, &configUpdate.Spec)
		if err != nil {
			return config, err
		}
		status := upgradeStatus.Status
		if status == nil {
			return config, fmt.Errorf("import cluster %s error: the cluster status is nil", *cluster.ClusterId)
		}
		if *status == ack.UpdateK8sRunningStatus {
			clusterIsUpgrading = true
		} else if *status == ack.UpdateK8sPauseStatus {
			pauseClusterUpgrade = true
		}
		configUpdate.Spec.PauseClusterUpgrade = pauseClusterUpgrade
		configUpdate.Spec.ClusterIsUpgrading = clusterIsUpgrading
	}
	configUpdate, err = h.ackCC.Update(configUpdate)
	if err != nil {
		return config, err
	}
	configStatus := configUpdate.DeepCopy()
	if err = h.createCASecret(configStatus, cluster, innerSecretStore); err != nil {
		return configStatus, err
	}
	configStatus.Status.Phase = ackConfigActivePhase
	return h.ackCC.UpdateStatus(configStatus)
}

func (h *Handler) OnAckConfigRemoved(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	if config.Spec.Imported {
		logrus.Infof("cluster [%s] is imported, will not delete ACK cluster", config.Name)
		return config, nil
	}
	if config.Status.Phase == ackConfigNotCreatedPhase {
		// The most likely context here is that the cluster already existed in ACK, so we shouldn't delete it
		logrus.Warnf("cluster [%s] never advanced to creating status, will not delete ACK cluster", config.Name)
		return config, nil
	}

	client, err := GetClient(h.secretsCache, &config.Spec, secretStore)
	if err != nil {
		return config, err
	}

	_, err = GetCluster(h.secretsCache, &config.Spec, secretStore)
	if err != nil {
		logrus.Infof("Get Cluster %v error: %+v", config.Spec.Name, err)
		if IsNotFound(err) {
			logrus.Infof("Cluster %v , region %v already removed", config.Spec.Name, config.Spec.RegionID)
			return config, nil
		}
		return config, err
	}

	logrus.Infof("removing cluster %v , region %v", config.Spec.Name, config.Spec.RegionID)
	if err := ack.RemoveCluster(client, &config.Spec); err != nil {
		logrus.Debugf("error deleting cluster %s: %v", config.Spec.Name, err)
		return config, err
	}

	return config, nil
}

func (h *Handler) create(config *ackv1.ACKClusterConfig, innerSecretStore *store.StsTokenStore) (*ackv1.ACKClusterConfig, error) {
	if config.Spec.Imported {
		logrus.Infof("importing cluster [%s]", config.Name)
		config = config.DeepCopy()
		config.Status.Phase = ackConfigImportingPhase
		return h.ackCC.UpdateStatus(config)
	}

	client, err := GetClient(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}

	// create instance , if in retry logic skip call create api
	if config.Spec.ClusterID == "" {
		if err = ack.Create(client, &config.Spec); err != nil {
			return config, err
		}
	}

	configUpdate := config.DeepCopy()
	configUpdate, err = h.ackCC.Update(configUpdate)
	if err != nil {
		return config, err
	}
	config = configUpdate.DeepCopy()
	config.Status.Phase = ackConfigCreatingPhase
	config, err = h.ackCC.UpdateStatus(config)
	logrus.Infof("current cluster id:%s", config.Spec.ClusterID)
	return config, err
}

func (h *Handler) checkAndUpdate(config *ackv1.ACKClusterConfig, innerSecretStore *store.StsTokenStore) (*ackv1.ACKClusterConfig, error) {
	cluster, err := GetClusterWithParam(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}
	if cluster == nil {
		return config, fmt.Errorf("update cluster error: the cluster is nil, indicating no cluster information is available")
	}
	clusterState := utils.GetMapString("state", *cluster)
	logrus.Infof("ackconfig cluster refersh updating %s", config.Name)
	clusterIsUpgrading := false
	client, err := GetClient(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}
	if config.Spec.ClusterID != "" {
		upgradeStatus, err := ack.GetUpgradeStatus(client, &config.Spec)
		if err != nil {
			return config, err
		}
		status := upgradeStatus.Status
		if status == nil {
			return config, fmt.Errorf("update cluster %s error: the cluster status is nil", config.Spec.ClusterID)
		}
		if *status == ack.UpdateK8sRunningStatus {
			clusterIsUpgrading = true
		}
		if *status == ack.UpdateK8sPauseStatus {
			updateErr := errors.New(fmt.Sprintf(`{"%s":"The cluster upgrade has been pause"}`, ack.UpdateK8SError))
			return config, updateErr
		}
		if *status == ack.UpdateK8sFailStatus {
			updateErr := errors.New(fmt.Sprintf(`{"%s":"%s"}`, ack.UpdateK8SError, *upgradeStatus.ErrorMessage))
			return config, updateErr
		}
	}

	// ACK k8s version update
	if !clusterIsUpgrading &&
		!config.Spec.PauseClusterUpgrade &&
		!config.Spec.ClusterIsUpgrading &&
		(config.Status.Phase == ackConfigActivePhase || strings.Contains(config.Status.FailureMessage, ack.UpdateK8SVersionApiError)) {
		if config.Spec.KubernetesVersion != utils.GetMapString("current_version", *cluster) {
			config.Status.Phase = ackConfigUpdatingPhase
			if err = ack.UpgradeCluster(client, &config.Spec); err != nil {
				updateErr := errors.New(fmt.Sprintf(`{"%s":"%s"}`, ack.UpdateK8SVersionApiError, err.Error()))
				return config, updateErr
			}
			return h.ackCC.UpdateStatus(config)
		}
	}

	if clusterState == ack.ClusterStatusUpdating ||
		clusterState == ack.ClusterStatusScaling ||
		clusterState == ack.ClusterStatusRemoving ||
		clusterIsUpgrading {
		// upstream cluster is already updating, must wait until sending next update
		logrus.Infof("waiting for cluster [%s] to finish %s", config.Name, clusterState)
		if config.Status.Phase != ackConfigUpdatingPhase {
			config = config.DeepCopy()
			config.Status.Phase = ackConfigUpdatingPhase
			return h.ackCC.UpdateStatus(config)
		}
		h.ackEnqueueAfter(config.Namespace, config.Name, 30*time.Second)
		return config, nil
	}

	updateConfig := config.DeepCopy()
	// fix config fields
	fixedSpec := FixConfig(&config.Spec, *cluster)
	if fixedSpec == nil {
		return config, fmt.Errorf("update cluster error: failed to convert and fix the configuration")
	}
	updateConfig.Spec = *fixedSpec
	updateConfig, err = h.ackCC.Update(updateConfig)
	if err != nil {
		return config, err
	}
	config = updateConfig.DeepCopy()
	nodePoolsInfo, err := GetNodePools(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
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
		if status == ack.NodePoolStatusScaling || status == ack.NodePoolStatusDeleting || status == ack.NodePoolStatusInitial || status == ack.NodePoolStatusUpdating || status == ack.NodePoolStatusRemoving {
			if config.Status.Phase != ackConfigUpdatingPhase {
				config = config.DeepCopy()
				config.Status.Phase = ackConfigUpdatingPhase
				config, err = h.ackCC.UpdateStatus(config)
				if err != nil {
					return config, err
				}
			}
			logrus.Infof("waiting for cluster [%s] to update node pool [%s]", config.Name, *np.NodepoolInfo.Name)
			h.ackEnqueueAfter(config.Namespace, config.Name, 30*time.Second)
			return config, nil
		}
	}
	upstreamSpec, err := BuildUpstreamClusterState(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}

	return h.updateUpstreamClusterState(config, upstreamSpec, innerSecretStore)
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
func (h *Handler) updateUpstreamClusterState(config *ackv1.ACKClusterConfig, upstreamSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*ackv1.ACKClusterConfig, error) {
	client, err := GetClient(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return config, err
	}

	var changed ack.Status
	changed, err = ack.UpdateNodePoolBatch(client, &config.Spec)
	if err != nil {
		return config, err
	}
	if changed == ack.Changed {
		return h.setUpdatingPhase(config)
	}

	// no new updates, set to active
	if config.Status.Phase != ackConfigActivePhase {
		logrus.Infof("cluster [%s] finished updating", config.Name)
		configUpdate := config.DeepCopy()
		configUpdate, err = h.ackCC.Update(configUpdate)
		if err != nil {
			return config, err
		}
		config = configUpdate.DeepCopy()
		config.Status.Phase = ackConfigActivePhase
		return h.ackCC.UpdateStatus(config)
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

func (h *Handler) waitForCreationComplete(config *ackv1.ACKClusterConfig, innerSecretStore *store.StsTokenStore) (*ackv1.ACKClusterConfig, error) {
	cluster, err := GetCluster(h.secretsCache, &config.Spec, innerSecretStore)
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
		if err := h.createCASecret(config, cluster, innerSecretStore); err != nil {
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

func GetNodePools(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*ackapi.DescribeClusterNodePoolsResponseBody, error) {
	client, err := GetClient(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return nil, err
	}
	return ack.GetNodePools(client, configSpec)
}

func GetClient(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*sdk.Client, error) {
	ns, id := utils.Parse(configSpec.AliyunCredentialSecret)
	if aliyunCredentialSecret := configSpec.AliyunCredentialSecret; aliyunCredentialSecret != "" {
		secret, err := secretsCache.Get(ns, id)
		if err != nil {
			return nil, err
		}

		accessKeyBytes := secret.Data["aliyunecscredentialConfig-accessKeyId"]
		secretKeyBytes := secret.Data["aliyunecscredentialConfig-accessKeySecret"]
		if accessKeyBytes == nil || secretKeyBytes == nil {
			return nil, fmt.Errorf("invalid aliyun cloud credential")
		}

		return ack.GetACKClient(
			configSpec.RegionID,
			string(accessKeyBytes),
			string(secretKeyBytes),
			"",
		)
	} else if configSpec.AccountID != "" && configSpec.RoleName != "" {
		secrets, err := secretsCache.List("cattle-global-data", labels.NewSelector())
		if err != nil {
			return nil, err
		}
		for _, secret := range secrets {
			if val, exists := secret.Annotations["provisioning.cattle.io/pandaria-aliyun-sst"]; exists && val == "true" {

				if innerSecretStore == nil {
					return nil, fmt.Errorf("The StsTokenStore is nil get sts token error")
				}
				stsToken, exists := innerSecretStore.GetAk(secret.Name)
				if !exists || checkSTSToken(stsToken) != nil {
					// If the STS token is not found for the first time or checkSTSToken fails (possibly due to the STS token expiration).
					// then retrieve it again.
					if err := setSTStoMap(secret, configSpec.AccountID, configSpec.RoleName, configSpec.RegionID, innerSecretStore); err != nil {
						return nil, err
					}

					// Retrieve the STS token again.
					stsToken, _ = innerSecretStore.GetAk(secret.Name)
					if err := checkSTSToken(stsToken); err != nil {
						return nil, err
					}
				}
				return ack.GetACKClient(
					configSpec.RegionID,
					stsToken.AK,
					stsToken.SK,
					stsToken.Token,
				)
			}
		}

	}
	return nil, fmt.Errorf("error while getting aliyunCredentialSecret")
}

func getSTSCredential(client *sts.Client, accountID, roleName string) (accessKeyId, accessKeySecret, token string, err error) {
	roleArn := fmt.Sprintf("acs:ram::%s:role/%s", accountID, roleName)
	request := sts.CreateAssumeRoleRequest()
	request.Scheme = defaultAPIScheme
	request.RoleArn = roleArn
	request.RoleSessionName = roleName

	response, err := client.AssumeRole(request)
	if err != nil {
		logrus.Errorf("Error get sts token failure message: %s", err.Error())
		return "", "", "", err
	}
	return response.Credentials.AccessKeyId, response.Credentials.AccessKeySecret, response.Credentials.SecurityToken, nil
}

func GetCluster(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*ackapi.DescribeClusterDetailResponseBody, error) {
	client, err := GetClient(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return nil, err
	}

	return ack.GetCluster(client, configSpec)
}

func GetClusterWithParam(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*map[string]interface{}, error) {
	client, err := GetClient(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return nil, err
	}

	return ack.GetClusterWithParams(client, configSpec)
}

func BuildUpstreamClusterState(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*ackv1.ACKClusterConfigSpec, error) {
	if configSpec == nil {
		logrus.Warn("Warning BuildUpstreamClusterState: The 'configSpec' data is nil, the cluster's configSpec is not available")
		return configSpec, nil
	}
	cluster, err := GetCluster(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return configSpec, err
	}
	if cluster == nil {
		logrus.Warn("Warning BuildUpstreamClusterState: Get cluster is nil, indicating no cluster information is available")
		return configSpec, nil
	}
	pauseClusterUpgrade := false
	clusterIsUpgrading := false
	if configSpec.ClusterID != "" {
		client, err := GetClient(secretsCache, configSpec, innerSecretStore)
		if err != nil {
			return configSpec, err
		}
		upgradeStatus, err := ack.GetUpgradeStatus(client, configSpec)
		if err != nil {
			return configSpec, err
		}
		status := upgradeStatus.Status
		if status == nil {
			logrus.Warn("Warning BuildUpstreamClusterState: The cluster status is nil, indicating no cluster information is available")
			return configSpec, nil
		}
		if *status == ack.UpdateK8sRunningStatus {
			clusterIsUpgrading = true
		} else if *status == ack.UpdateK8sPauseStatus {
			pauseClusterUpgrade = true
		}
	}
	newSpec := &ackv1.ACKClusterConfigSpec{
		Name:                tea.StringValue(cluster.Name),
		ClusterID:           tea.StringValue(cluster.ClusterId),
		ClusterType:         tea.StringValue(cluster.ClusterType),
		KubernetesVersion:   tea.StringValue(cluster.CurrentVersion),
		RegionID:            tea.StringValue(cluster.RegionId),
		VpcID:               tea.StringValue(cluster.VpcId),
		PauseClusterUpgrade: pauseClusterUpgrade,
		ClusterIsUpgrading:  clusterIsUpgrading,
	}
	newSpec.NodePoolList, err = GetNodePoolConfigInfo(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return configSpec, err
	}

	return newSpec, nil
}

func GetNodePoolConfigInfo(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) ([]ackv1.NodePoolInfo, error) {
	nodePoolInfo, err := GetNodePools(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return nil, err
	}
	return ack.ToNodePoolConfigInfo(nodePoolInfo)
}

func GetUserConfig(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) (*ackapi.DescribeClusterUserKubeconfigResponseBody, error) {
	client, err := GetClient(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return nil, err
	}
	return ack.GetUserConfig(client, configSpec)
}

// FixConfig fix fields for imported clusters
func FixConfig(configSpec *ackv1.ACKClusterConfigSpec, clusterMap map[string]interface{}) *ackv1.ACKClusterConfigSpec {
	// update known field from query result
	configSpec.ClusterType = utils.GetMapString("cluster_type", clusterMap)
	if configSpec.KubernetesVersion == "" {
		configSpec.KubernetesVersion = utils.GetMapString("current_version", clusterMap)
	}
	configSpec.Name = utils.GetMapString("name", clusterMap)
	configSpec.VswitchIds = strings.Split(utils.GetMapString("vswitch_id", clusterMap)+"", ",") // append empty string, avoid empty pointer value
	configSpec.ResourceGroupID = utils.GetMapString("resource_group_id", clusterMap)
	// only can get these params while state is active
	var (
		params  map[string]interface{}
		outputs []interface{}
	)
	if clusterMap["parameters"] != nil {
		params = clusterMap["parameters"].(map[string]interface{})
	}
	if clusterMap["outputs"] != nil {
		outputs = clusterMap["outputs"].([]interface{})
	}

	if params != nil {
		// for masters
		if configSpec.ClusterType == "Kubernetes" {
			configSpec.MasterCount = utils.GetMapInt64("MasterCount", params)
			configSpec.MasterInstanceTypes = strings.Split(utils.GetMapString("MasterInstanceTypes", params), ",")
			configSpec.MasterInstanceChargeType = utils.GetMapString("MasterInstanceChargeType", params)
			configSpec.MasterPeriod = utils.GetMapInt64("MasterPeriod", params)
			configSpec.MasterPeriodUnit = utils.GetMapString("MasterPeriodUnit", params)
			configSpec.MasterAutoRenew = utils.GetMapBoolean("MasterAutoRenew", params)
			configSpec.MasterAutoRenewPeriod = utils.GetMapInt64("MasterAutoRenewPeriod", params)
			configSpec.MasterSystemDiskCategory = utils.GetMapString("MasterSystemDiskCategory", params)
			configSpec.MasterSystemDiskSize = utils.GetMapInt64("MasterSystemDiskSize", params)
			configSpec.MasterVswitchIds = strings.Split(utils.GetMapString("MasterVSwitchIds", params), ",")
		}
		if len(configSpec.MasterVswitchIds) == 0 { // display on ui, can not be empty
			configSpec.MasterVswitchIds = configSpec.VswitchIds
		}

		configSpec.ResourceGroupID = utils.GetMapString("ResourceGroupId", params)
		configSpec.ContainerCidr = utils.GetMapString("ContainerCIDR", params)
		configSpec.ServiceCidr = utils.GetMapString("ServiceCIDR", params)
		configSpec.VpcID = utils.GetMapString("VpcId", params)

		configSpec.SnatEntry = configSpec.SnatEntry || utils.GetMapBoolean("SNatEntry", params)
		configSpec.EndpointPublicAccess = configSpec.EndpointPublicAccess || utils.GetMapBoolean("Eip", params)

		// SetUpArgs --node-cidr-mask 26
		nodeCidrMask := utils.GetArgValueByKey("--node-cidr-mask", utils.GetMapString("SetUpArgs", params))
		if nodeCidrMask != "" {
			maskNum, err := strconv.Atoi(nodeCidrMask)
			if err != nil {
				logrus.Warnf("get node-cidr-mask failed:%s", nodeCidrMask)
			} else {
				configSpec.NodeCidrMask = int64(maskNum)
			}
		}
	}

	for _, output := range outputs {
		key := output.(map[string]interface{})["OutputKey"]
		if key != nil {
			if key.(string) == "ProxyMode" {
				if value := output.(map[string]interface{})["OutputValue"]; value != nil {
					configSpec.ProxyMode = value.(string)
				}
			}
		}
	}

	return configSpec
}

// FixClusterId fix empty field clusterId , only for clusters which create by ack-operator
func FixClusterId(secretsCache wranglerv1.SecretCache, configSpec *ackv1.ACKClusterConfigSpec, innerSecretStore *store.StsTokenStore) error {
	client, err := GetClient(secretsCache, configSpec, innerSecretStore)
	if err != nil {
		return err
	}

	clusters, err := ack.GetClusters(client, configSpec)
	if err != nil {
		return err
	}
	if clusters == nil || clusters.Clusters == nil {
		return fmt.Errorf("fix cluster id error: Get the clusters is nil, indicating no cluster information is available")
	}
	if len(clusters.Clusters) == 1 {
		if *clusters.Clusters[0].Name == configSpec.Name {
			configSpec.ClusterID = *clusters.Clusters[0].ClusterId
		} else {
			logrus.Warnf("error while fix cluster id ,cluster name get :%s,but excped is %s", *clusters.Clusters[0].Name, configSpec.Name)
		}
	} else {
		// return error will block process to return error message
		logrus.Warnf("error while fix cluster id ,return unexceptd cluster(s):%d", len(clusters.Clusters))
	}

	return nil
}

// createCASecret creates a secret containing a CA and endpoint for use in generating a kubeconfig file.
func (h *Handler) createCASecret(config *ackv1.ACKClusterConfig, cluster *ackapi.DescribeClusterDetailResponseBody, innerSecretStore *store.StsTokenStore) error {
	client, err := GetClient(h.secretsCache, &config.Spec, innerSecretStore)
	if err != nil {
		return err
	}

	request := requests.NewCommonRequest()
	request.Method = "GET"
	request.Scheme = defaultAPIScheme
	request.Domain = "cs." + config.Spec.RegionID + ".aliyuncs.com"
	request.Version = ack.DefaultACKAPIVersion
	request.PathPattern = "/k8s/" + config.Spec.ClusterID + "/user_config"
	request.Headers["Content-Type"] = "application/json"

	body := `{}`
	request.Content = []byte(body)

	response, err := client.ProcessCommonRequest(request)
	if err != nil {
		return err
	}

	kubeConfig := &ackapi.DescribeClusterUserKubeconfigResponseBody{}
	if err = json.Unmarshal(response.GetHttpContentBytes(), kubeConfig); err != nil {
		return err
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(*kubeConfig.Config))
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

func IsNotFound(err error) bool {
	if strings.Contains(err.Error(), "ErrorClusterNotFound") {
		return true
	}
	return false
}

func checkSTSToken(stsToken store.StsToken) error {
	client, err := ecs.NewClientWithStsToken(
		defaultRegion,
		stsToken.AK,
		stsToken.SK,
		stsToken.Token,
	)
	if err != nil {
		return err
	}
	request := ecs.CreateDescribeRegionsRequest()
	request.Scheme = defaultAPIScheme

	_, err = client.DescribeRegions(request)
	if err != nil {
		return err
	}
	return nil
}

func setSTStoMap(cc *corev1.Secret, accountID, roleName, regionID string, innerSecretStore *store.StsTokenStore) error {
	stsAk := string(cc.Data["aliyunecscredentialConfig-accessKeyId"])
	stsSk := string(cc.Data["aliyunecscredentialConfig-accessKeySecret"])
	client, err := getACKSTSClient(regionID, stsAk, stsSk)
	if err != nil {
		return fmt.Errorf("error get sts token error secret Name: %s", cc.Name)
	}
	ak, sk, token, err := getSTSCredential(client, accountID, roleName)
	if err != nil {
		return fmt.Errorf("error get sts token error secret Name: %s", cc.Name)
	}
	if innerSecretStore == nil {
		return fmt.Errorf("error get sts token error secretStore is nil: %s", cc.Name)
	}
	innerSecretStore.SetAk(cc.Name, store.StsToken{
		AK:    ak,
		SK:    sk,
		Token: token,
	})
	return nil
}

func getACKSTSClient(regionID, accessKeyID, accessKeySecret string) (*sts.Client, error) {
	if regionID == "" {
		regionID = defaultRegion
	}
	config := sdk.NewConfig()
	credential := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
	return sts.NewClientWithOptions(regionID, config, credential)
}

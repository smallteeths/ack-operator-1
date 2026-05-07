package controller

import (
	"fmt"
	"time"

	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"github.com/sirupsen/logrus"
	kubeWait "k8s.io/apimachinery/pkg/util/wait"
)

var ackRemoveBackoff = kubeWait.Backoff{
	Duration: 5 * time.Second,
	Factor:   2,
	Steps:    12,
}

func (h *Handler) OnAckConfigRemoved(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	logrus.Infof("handler ACK cluster remove...")
	if config.Spec.Imported {
		logrus.Infof("cluster [%s] is imported, will not delete ACK cluster", config.Name)
		return config, nil
	}
	if config.Spec.DeletionProtection {
		err := fmt.Errorf("ACK cluster [%s] deletion is blocked because deletion protection is enabled in Rancher", config.Name)
		logrus.Info(err.Error())
		h.recordRemoveError(config, err.Error())
		return config, err
	}
	client, err := ack.NewACKClient(h.secretsCache, &config.Spec)
	if err != nil {
		return config, err
	}
	ackCluster, err := ack.DescribeACKCluster(h.secretsCache, &config.Spec)
	if err != nil {
		logrus.Infof("get ACK cluster %v error: %+v", config.Spec.Name, err)
		if ack.IsNotFound(err) {
			logrus.Infof("ACK cluster %v, region %v already removed", config.Spec.Name, config.Spec.RegionID)
			h.recordRemoveError(config, "")
			return config, nil
		}
		return config, err
	}
	if ackCluster.DeletionProtection != nil && *ackCluster.DeletionProtection {
		err := fmt.Errorf("ACK cluster [%s] deletion is blocked because deletion protection is enabled on Alibaba Cloud", config.Name)
		logrus.Info(err.Error())
		h.recordRemoveError(config, err.Error())
		return config, err
	}
	h.recordRemoveError(config, "")
	if err := kubeWait.ExponentialBackoff(ackRemoveBackoff, func() (bool, error) {
		client, err = ack.NewACKClient(h.secretsCache, &config.Spec)
		if err != nil {
			return false, err
		}
		ackCluster, err = ack.DescribeACKCluster(h.secretsCache, &config.Spec)
		if err != nil {
			logrus.Infof("get ACK cluster %v error: %+v", config.Spec.Name, err)
			if ack.IsNotFound(err) {
				logrus.Infof("ACK cluster %v, region %v already removed", config.Spec.Name, config.Spec.RegionID)
				return true, nil
			}
			return false, err
		}
		if ackCluster.DeletionProtection != nil && *ackCluster.DeletionProtection {
			err := fmt.Errorf("ACK cluster [%s] deletion is blocked because deletion protection is enabled on Alibaba Cloud", config.Name)
			logrus.Info(err.Error())
			h.recordRemoveError(config, err.Error())
			return false, err
		}
		logrus.Infof("removing ACK cluster %v, region %v", config.Spec.Name, config.Spec.RegionID)
		if err := ack.RemoveACKCluster(client, &config.Spec); err != nil {
			if ack.IsNotFound(err) {
				logrus.Infof("ACK cluster %v, region %v already removed", config.Spec.Name, config.Spec.RegionID)
				return true, nil
			}
			logrus.Errorf("failed to delete ACK cluster [%s]: %v", config.Spec.Name, err)
			return false, err
		}
		logrus.Infof("ACK cluster %v deletion requested successfully", config.Spec.Name)
		return true, nil
	}); err != nil {
		return config, err
	}
	return config, nil
}

func (h *Handler) recordRemoveError(config *ackv1.ACKClusterConfig, message string) {
	if config == nil || config.Status.FailureMessage == message {
		return
	}
	configCopy := config.DeepCopy()
	configCopy.Status.Phase = ackConfigUpdatingPhase
	configCopy.Status.FailureMessage = message
	if _, err := h.ackCC.UpdateStatus(configCopy); err != nil {
		logrus.Errorf("error recording dddd [%s] remove failure message: %s", config.Name, err.Error())
	}
}

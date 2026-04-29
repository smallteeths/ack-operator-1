package controller

import (
	"fmt"

	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"github.com/sirupsen/logrus"
)

func (h *Handler) OnAckConfigRemoved(key string, config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	if config.Spec.Imported {
		logrus.Infof("cluster [%s] is imported, will not delete ACK cluster", config.Name)
		return config, nil
	}
	if config.Spec.DeletionProtection {
		err := fmt.Errorf("ACK cluster [%s] deletion is blocked because deletion protection is enabled in Rancher", config.Name)

		logrus.Infof(err.Error())

		return config, err
	}
	if config.Status.Phase == ackConfigNotCreatedPhase {
		// The most likely context here is that the cluster already existed in ACK, so we shouldn't delete it
		logrus.Warnf("cluster [%s] never advanced to creating status, will not delete ACK cluster", config.Name)
		return config, nil
	}
	client, err := ack.NewACKClient(h.secretsCache, &config.Spec)
	if err != nil {
		return config, err
	}
	ackCluster, err := ack.DescribeACKCluster(h.secretsCache, &config.Spec)
	if err != nil {
		logrus.Infof("Get Cluster %v error: %+v", config.Spec.Name, err)
		if ack.IsNotFound(err) {
			logrus.Infof("Cluster %v , region %v already removed", config.Spec.Name, config.Spec.RegionID)
			return config, nil
		}

		return config, err
	}
	if ackCluster.DeletionProtection != nil && *ackCluster.DeletionProtection {
		err := fmt.Errorf("ACK cluster [%s] deletion is blocked because deletion protection is enabled on Alibaba Cloud", config.Name)

		logrus.Infof(err.Error())

		return config, err
	}
	logrus.Infof("removing cluster %v , region %v", config.Spec.Name, config.Spec.RegionID)
	if err := ack.RemoveACKCluster(client, &config.Spec); err != nil {
		logrus.Debugf("error deleting cluster %s: %v", config.Spec.Name, err)
		return config, err
	}

	return config, nil
}

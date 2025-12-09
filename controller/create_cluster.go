package controller

import (
	"github.com/cnrancher/ack-operator/internal/ack"
	ackv1 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"
	"github.com/sirupsen/logrus"
)

func (h *Handler) create(config *ackv1.ACKClusterConfig) (*ackv1.ACKClusterConfig, error) {
	if config.Spec.Imported {
		logrus.Infof("importing cluster [%s]", config.Name)
		config = config.DeepCopy()
		config.Status.Phase = ackConfigImportingPhase
		return h.ackCC.UpdateStatus(config)
	}
	client, err := ack.NewACKClient(h.secretsCache, &config.Spec)
	if err != nil {
		return config, err
	}
	// create instance , if in retry logic skip call create api
	if config.Spec.ClusterID == "" {
		if err = ack.CreateACK(client, &config.Spec); err != nil {
			return config, err
		}
	}
	configUpdate := config.DeepCopy()
	// 更新 clusterID
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

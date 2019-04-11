package main

import (
	"reflect"

	"github.com/golang/glog"

	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	webhookName = "serve.k8s-metadata-injector.io"
)

func (wh *Webhook) selfRegistration(webhookConfigName string) error {
	client := wh.clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	existing, getErr := client.Get(webhookConfigName, metav1.GetOptions{})
	if getErr != nil && !errors.IsNotFound(getErr) {
		return getErr
	}

	ignorePolicy := v1beta1.Ignore
	caCert, err := readCertFile(wh.cert.caCertFile)
	if err != nil {
		return err
	}
	webhook := v1beta1.Webhook{
		Name: webhookName,
		Rules: []v1beta1.RuleWithOperations{
			{
				Operations: []v1beta1.OperationType{v1beta1.Create},
				Rule: v1beta1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods", "services", "persistentvolumeclaims"},
				},
			},
			{
				Operations: []v1beta1.OperationType{v1beta1.Update},
				Rule: v1beta1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"services", "persistentvolumeclaims"},
				},
			},
		},
		ClientConfig: v1beta1.WebhookClientConfig{
			Service:  wh.serviceRef,
			CABundle: caCert,
		},
		FailurePolicy: &ignorePolicy,
	}
	webhooks := []v1beta1.Webhook{webhook}

	if getErr == nil && existing != nil {
		// Update case.
		glog.Info("Updating existing MutatingWebhookConfiguration for the k8s-metadata-injector admission webhook")
		if !reflect.DeepEqual(webhooks, existing.Webhooks) {
			existing.Webhooks = webhooks
			if _, err := client.Update(existing); err != nil {
				return err
			}
		}
	} else {
		// Create case.
		glog.Info("Creating a MutatingWebhookConfiguration for the k8s-metadata-injector admission webhook")
		webhookConfig := &v1beta1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: webhookConfigName,
			},
			Webhooks: webhooks,
		}
		if _, err := client.Create(webhookConfig); err != nil {
			return err
		}
	}

	return nil
}

func (wh *Webhook) selfDeregistration(webhookConfigName string) error {
	client := wh.clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	return client.Delete(webhookConfigName, metav1.NewDeleteOptions(0))
}

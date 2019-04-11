package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/client-go/kubernetes"
	//admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	//"k8s.io/kubernetes/pkg/apis/core/v1"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

var ignoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

const (
	admissionWebhookAnnotationInjectKey = "k8s-metadata-injector.kubernetes.io/skip"
	admissionWebhookAnnotationStatusKey = "k8s-metadata-injector.kubernetes.io/status"

	serverCertFile = "server-cert.pem"
	serverKeyFile  = "server-key.pem"
	caCertFile     = "ca-cert.pem"
)

type Webhook struct {
	clientset      kubernetes.Interface
	server         *http.Server
	cert           *certBundle
	serviceRef     *v1beta1.ServiceReference
	metadataConfig *MetadataConfig
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func NewWebhook(
	clientset kubernetes.Interface,
	certDir string,
	webhookServiceNamespace string,
	webhookServiceName string,
	webhookPort int,
	metadataConfig *MetadataConfig) (*Webhook, error) {

	cert := &certBundle{
		serverCertFile: filepath.Join(certDir, serverCertFile),
		serverKeyFile:  filepath.Join(certDir, serverKeyFile),
		caCertFile:     filepath.Join(certDir, caCertFile),
	}
	path := "/serve"
	serviceRef := &v1beta1.ServiceReference{
		Namespace: webhookServiceNamespace,
		Name:      webhookServiceName,
		Path:      &path,
	}
	hook := &Webhook{
		clientset:      clientset,
		cert:           cert,
		serviceRef:     serviceRef,
		metadataConfig: metadataConfig,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(path, hook.serve)
	tlsConfig, err := configServerTLS(cert)
	if err != nil {
		return nil, err
	}
	hook.server = &http.Server{
		Addr:      fmt.Sprintf(":%d", webhookPort),
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	return hook, nil
}

// Start starts the admission webhook server and registers itself to the API server.
func (wh *Webhook) Start(webhookConfigName string) error {
	go func() {
		glog.Info("Starting the k8s-metadata-injector admission webhook server")
		if err := wh.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			glog.Errorf("error while serving the k8s-metadata-injector admission webhook: %v\n", err)
		}
	}()

	return wh.selfRegistration(webhookConfigName)
}

// Stop deregisters itself with the API server and stops the admission webhook server.
func (wh *Webhook) Stop(webhookConfigName string) error {
	if err := wh.selfDeregistration(webhookConfigName); err != nil {
		return err
	}
	glog.Infof("Webhook %s deregistered", webhookConfigName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	glog.Info("Stopping the k8s-metadata-injector admission webhook server")
	return wh.server.Shutdown(ctx)
}

func (wh *Webhook) serve(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("Serving admission request")
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			if err != nil {
				glog.Errorf("failed to read the request body")
				http.Error(w, "failed to read the request body", http.StatusInternalServerError)
				return
			}
			body = data
		}
	}

	if len(body) == 0 {
		glog.Error("empty request body")
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *admissionv1beta1.AdmissionResponse
	ar := admissionv1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Errorf("Can't decode body: %v", err)
		admissionResponse = &admissionv1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = wh.mutate(&ar)
	}

	admissionReview := admissionv1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		glog.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	glog.Infof("Ready to write reponse ...")
	if _, err := w.Write(resp); err != nil {
		glog.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}

}

func (wh *Webhook) mutate(ar *admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {

	req := ar.Request

	var metadataConfig map[string]MetadataSpec
	var metadata *metav1.ObjectMeta

	if req.Kind.Kind == "Pod" {

		metadataConfig = wh.metadataConfig.Pod

		var pod corev1.Pod
		if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &admissionv1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}

		metadata = &pod.ObjectMeta

	} else if req.Kind.Kind == "Service" {

		metadataConfig = wh.metadataConfig.Service

		var service corev1.Service
		if err := json.Unmarshal(req.Object.Raw, &service); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &admissionv1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}

		metadata = &service.ObjectMeta

	} else if req.Kind.Kind == "PersistentVolumeClaim" {

		metadataConfig = wh.metadataConfig.PersistentVolumeClaim

		var pvc corev1.PersistentVolumeClaim
		if err := json.Unmarshal(req.Object.Raw, &pvc); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &admissionv1beta1.AdmissionResponse{
				Result: &metav1.Status{
					Message: err.Error(),
				},
			}
		}

		metadata = &pvc.ObjectMeta

	} else {
		// nothing
	}

	glog.Infof("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, metadata.Name, req.UID, req.Operation, req.UserInfo)

	// Deal with potential empty fields, e.g., when the pod is created by a deployment
	//podName := potentialPodName(&pod.ObjectMeta)
	if metadata.Namespace == "" {
		metadata.Namespace = req.Namespace
	}

	// check if namespace exist in mutationCongfig

	// determine whether to perform mutation
	if !mutationRequired(ignoredNamespaces, metadataConfig, metadata) {
		glog.Infof("Skipping mutation for %s/%s due to policy check", metadata.Namespace, metadata.Name)
		return &admissionv1beta1.AdmissionResponse{
			Allowed: true,
		}
	}

	annotations := map[string]string{admissionWebhookAnnotationStatusKey: "injected"}
	patchBytes, err := createPatch(metadata, metadataConfig, annotations)
	if err != nil {
		return &admissionv1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &admissionv1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1beta1.PatchType {
			pt := admissionv1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func createPatch(metadata *metav1.ObjectMeta, metadataConfig map[string]MetadataSpec, annotations map[string]string) ([]byte, error) {
	var patch []patchOperation

	if podMeta, ok := metadataConfig[metadata.Namespace]; ok {
		for k, v := range podMeta.Annotations {
			annotations[k] = v
		}
		patch = append(patch, updateAnnotation(metadata.Annotations, annotations)...)
		patch = append(patch, updateLabels(metadata.Labels, podMeta.Labels)...)
	} else {
		patch = append(patch, updateAnnotation(metadata.Annotations, annotations)...)
	}

	return json.Marshal(patch)
}

func updateAnnotation(target map[string]string, added map[string]string) (patch []patchOperation) {

	if target == nil {
		target = map[string]string{}
	}

	for key, value := range added {
		target[key] = value
	}

	patch = append(patch, patchOperation{
		Op:    "add",
		Path:  "/metadata/annotations",
		Value: target,
	})
	return patch
}

func updateLabels(target map[string]string, added map[string]string) (patch []patchOperation) {

	if target == nil {
		target = map[string]string{}
	}

	for key, value := range added {
		target[key] = value
	}

	patch = append(patch, patchOperation{
		Op:    "add",
		Path:  "/metadata/labels",
		Value: target,
	})
	return patch
}

func mutationRequired(ignoredList []string, metadataConfig map[string]MetadataSpec, metadata *metav1.ObjectMeta) bool {
	// skip special kubernete system namespaces
	for _, namespace := range ignoredList {
		if metadata.Namespace == namespace {
			glog.Infof("Skip mutation for %v for it' in special namespace:%v", metadata.Name, metadata.Namespace)
			return false
		}
	}

	if _, ok := metadataConfig[metadata.Namespace]; !ok {
		glog.Infof("Skip mutation for %v for it is not configured in mutaion config:%v", metadata.Name, metadata.Namespace)
		return false
	}

	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	status := annotations[admissionWebhookAnnotationStatusKey]

	// determine whether to perform mutation based on annotation for the target resource
	var required bool
	switch strings.ToLower(annotations[admissionWebhookAnnotationInjectKey]) {
	default:
		required = true
	case "y", "yes", "true", "on":
		required = false
	}

	glog.Infof("Mutation policy for %v/%v: status: %q required:%v", metadata.Namespace, metadata.Name, status, required)
	return required
}

func potentialPodName(metadata *metav1.ObjectMeta) string {
	if metadata.Name != "" {
		return metadata.Name
	}
	if metadata.GenerateName != "" {
		return metadata.GenerateName + "***** (actual name not yet known)"
	}
	return ""
}

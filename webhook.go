package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
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

	// (https://github.com/kubernetes/kubernetes/issues/57982)
	//defaulter = runtime.ObjectDefaulter(runtimeScheme)
)

var ignoredNamespaces = []string {
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

const (
	admissionWebhookAnnotationInjectKey = "k8s-metadata-injector.kubernetes.io/skip"
	admissionWebhookAnnotationStatusKey = "k8s-metadata-injector.kubernetes.io/status"
)

type WhSvrParameters struct {
	port int                 // webhook server port
	certFile string          // path to the x509 certificate for https
	keyFile string           // path to the x509 private key matching `CertFile`
	metadataCfgFile string    // path to sidecar injector configuration file
}

type WebhookServer struct {
	server           *http.Server
	metadataConfig   *Config
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func loadConfig(configFile string) (*Config, error) {
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (whsvr *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {

  var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		glog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

  // verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

  var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1beta1.AdmissionResponse {
			Result: &metav1.Status {
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = whsvr.mutate(&ar)
	}

  admissionReview := v1beta1.AdmissionReview{}
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

func (whsvr *WebhookServer) mutate(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {

	req := ar.Request

	var metadataConfig map[string]MetadataSpec
	var metadata *metav1.ObjectMeta

	if req.Kind.Kind == "Pod" {

		metadataConfig = whsvr.metadataConfig.Pod

		var pod corev1.Pod
		if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse {
				Result: &metav1.Status {
					Message: err.Error(),
				},
			}
		}

		metadata = &pod.ObjectMeta

	} else if req.Kind.Kind == "Service" {

		metadataConfig = whsvr.metadataConfig.Service

		var service corev1.Service
		if err := json.Unmarshal(req.Object.Raw, &service); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse {
				Result: &metav1.Status {
					Message: err.Error(),
				},
			}
		}

		metadata = &service.ObjectMeta

	} else if req.Kind.Kind == "PersistentVolumeClaim" {

		metadataConfig = whsvr.metadataConfig.PersistentVolumeClaim

		var pvc corev1.PersistentVolumeClaim
		if err := json.Unmarshal(req.Object.Raw, &pvc); err != nil {
			glog.Errorf("Could not unmarshal raw object: %v", err)
			return &v1beta1.AdmissionResponse {
				Result: &metav1.Status {
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
		return &v1beta1.AdmissionResponse {
			Allowed: true,
		}
	}

	annotations := map[string]string{admissionWebhookAnnotationStatusKey: "injected"}
	patchBytes, err := createPatch(metadata, metadataConfig, annotations)
	if err != nil {
		return &v1beta1.AdmissionResponse {
			Result: &metav1.Status {
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &v1beta1.AdmissionResponse {
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1beta1.PatchType {
			pt := v1beta1.PatchTypeJSONPatch
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

	patch = append(patch, patchOperation {
		Op:   "add",
		Path: "/metadata/annotations",
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

	patch = append(patch, patchOperation {
		Op:   "add",
		Path: "/metadata/labels",
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

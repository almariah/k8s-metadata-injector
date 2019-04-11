package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"k8s.io/klog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	master              = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeConfig          = flag.String("kubeConfig", "", "Path to a kube config. Only required if out-of-cluster.")
	webhookConfigName   = flag.String("webhook-config-name", "k8s-metadata-injector", "The name of the MutatingWebhookConfiguration object to create.")
	webhookCertDir      = flag.String("webhook-cert-dir", "/etc/webhook/certs/", "The directory where x509 certificate and key files are stored.")
	webhookSvcNamespace = flag.String("webhook-svc-namespace", "kube-system", "The namespace of the Service for the webhook server.")
	webhookSvcName      = flag.String("webhook-svc-name", "k8s-metadata-injector", "The name of the Service for the webhook server.")
	webhookPort         = flag.Int("webhook-port", 8080, "Service port of the webhook server.")
	metadataConfigFile  = flag.String("metadata-config-file", "/etc/webhook/config/metadataconfig.yaml", "File containing the metadata configuration.")
)

func main() {

	flag.Set("alsologtostderr", "true")
	flag.Set("stderrthreshold", "info")
	flag.Set("v", "2")

	flag.Parse()

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			f2.Value.Set(value)
		}
	})

	cfg, err := clientcmd.BuildConfigFromFlags(*master, *kubeConfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	stopCh := make(chan struct{})

	controller := NewController(kubeClient)

	if err = controller.Run(2, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}

	metadataConfig, err := loadConfig(*metadataConfigFile)
	if err != nil {
		klog.Errorf("Filed to load configuration: %v", err)
	}

	hook, err := NewWebhook(kubeClient, *webhookCertDir, *webhookSvcNamespace, *webhookSvcName, *webhookPort, metadataConfig)
	if err != nil {
		klog.Fatal(err)
	}

	if err = hook.Start(*webhookConfigName); err != nil {
		klog.Fatal(err)
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	<-signalCh

	close(stopCh)

	glog.Info("Shutting down the k8s-metadata-injector")
	if err := hook.Stop(*webhookConfigName); err != nil {
		klog.Fatal(err)
	}

}

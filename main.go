package main

import (
  "fmt"
  "flag"

  "crypto/tls"
  "net/http"

  "k8s.io/klog"
	"github.com/golang/glog"

  "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)


var (
	masterURL   string
	kubeconfig  string
)

func main() {

  var parameters WhSvrParameters

	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")

  flag.IntVar(&parameters.port, "port", 443, "Webhook server port.")
	flag.StringVar(&parameters.certFile, "tlsCertFile", "/etc/webhook/certs/cert.pem", "File containing the x509 Certificate for HTTPS.")
	flag.StringVar(&parameters.keyFile, "tlsKeyFile", "/etc/webhook/certs/key.pem", "File containing the x509 private key to --tlsCertFile.")
  flag.StringVar(&parameters.metadataCfgFile, "metadataCfgFile", "/etc/webhook/config/metadataconfig.yaml", "File containing the mutation configuration.")

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

  cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

  metadataConfig, err := loadConfig(parameters.metadataCfgFile)
	if err != nil {
		glog.Errorf("Filed to load configuration: %v", err)
	}

  pair, err := tls.LoadX509KeyPair(parameters.certFile, parameters.keyFile)
	if err != nil {
		glog.Errorf("Filed to load key pair: %v", err)
	}
	whsvr := &WebhookServer {
		metadataConfig:    metadataConfig,
		server:           &http.Server {
			Addr:        fmt.Sprintf(":%v", parameters.port),
			TLSConfig:   &tls.Config{Certificates: []tls.Certificate{pair}},
		},
	}
  mux := http.NewServeMux()
	mux.HandleFunc("/serve", whsvr.serve)
	whsvr.server.Handler = mux
  go func() {
		if err := whsvr.server.ListenAndServeTLS("", ""); err != nil {
			glog.Errorf("Failed to listen and serve webhook server: %v", err)
		}
	}()


  //signalChan := make(chan os.Signal, 1)
	//signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	//<-signalChan

  stopCh := make(chan struct{})

  controller := NewController(client)

  if err = controller.Run(2, stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}

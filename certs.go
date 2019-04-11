package main

import (
	"crypto/tls"
	"io/ioutil"
)

// certBundle is a container of a X509 certificate file and a corresponding key file for the
// webhook server, and a CA certificate file for the API server to verify the server certificate.
type certBundle struct {
	serverCertFile string
	serverKeyFile  string
	caCertFile     string
}

// configServerTLS configures TLS for the admission webhook server.
func configServerTLS(certBundle *certBundle) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certBundle.serverCertFile, certBundle.serverKeyFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

func readCertFile(certFile string) ([]byte, error) {
	return ioutil.ReadFile(certFile)
}

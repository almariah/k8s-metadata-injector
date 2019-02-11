#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

export CA_BUNDLE=$(kubectl get configmap -n kube-system extension-apiserver-authentication -o=jsonpath='{.data.client-ca-file}' | base64 | tr -d '\n')

if [[ ${CA_BUNDLE} == '' ]]; then
  >&2 echo "Warning: client-ca-file does not exist in extension-apiserver-authentication!"
  >&2 echo "Info: trying to get CA certificate from kubeconfig ..."
  export CA_BUNDLE=$(kubectl config view --raw --flatten -o=jsonpath='{.clusters[?(@.name=="'$(kubectl config current-context)'")].cluster.certificate-authority-data}')
fi

if [[ ${CA_BUNDLE} == '' ]]; then
  echo "Error: could't get CA certificate"
  exit 1
fi

if command -v envsubst >/dev/null 2>&1; then
    envsubst
else
    sed -e "s|\${CA_BUNDLE}|${CA_BUNDLE}|g"
fi

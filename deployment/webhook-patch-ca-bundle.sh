#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

export CA_BUNDLE=$(kubectl get secret k8s-metadata-injector -n kube-system -o jsonpath='{.data.cert\.pem}')

if command -v envsubst >/dev/null 2>&1; then
    envsubst
else
    sed -e "s|\${CA_BUNDLE}|${CA_BUNDLE}|g"
fi

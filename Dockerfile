ARG IMAGE=alpine:3.9.3

FROM golang:1.12.3-alpine as builder
WORKDIR ${GOPATH}/src/github.com/alamriah/k8s-metadata-injector
COPY . ./
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o /usr/bin/k8s-metadata-injector

FROM ${IMAGE}
RUN apk add --no-cache bash openssl curl
COPY --from=builder /usr/bin/k8s-metadata-injector /usr/bin/
COPY hack/gencerts.sh /usr/bin/
ENTRYPOINT ["/usr/bin/k8s-metadata-injector"]

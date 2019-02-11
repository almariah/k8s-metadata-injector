FROM scratch

ADD ca-certificates.crt /etc/ssl/certs/

COPY k8s-metadata-injector /k8s-metadata-injector

ENTRYPOINT ["/k8s-metadata-injector"]

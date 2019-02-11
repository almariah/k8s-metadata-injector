FROM scratch

COPY k8s-metadata-injector /k8s-metadata-injector
ADD ca-certificates.crt /etc/ssl/certs/


ENTRYPOINT ["/k8s-metadata-injector"]

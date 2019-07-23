# k8s-metadata-injector

## Introduction

Labels and annotations are important to classify Kubernetes resources. Further, there are some annotations that will tag the corresponding AWS resources created by the Kubernetes itself.

The `k8s-metadata-injector` has two goals:

* Inject additional labels and annotations to `pods`, `services` and `persistentvolumeclaims` based on predefined config per namespace.
* Add tags to created AWS EBS volumes created by `persistentvolumeclaims` (`-ebs-tagging=true` should be configured)

You can add tags to EBS volumes by setting the following annotations in `persistentVolumeClaim` or `volumeClaimTemplate` as follow (example):

```yaml
ebs-tagger.kubernetes.io/ebs-additional-resource-tags: "Team=devops,Env=prod,Project=k8s"
```

Further you can automatically inject this annotation into any `persistentVolumeClaim` or `volumeClaimTemplate`
by adding the annotation in `persistentVolumeClaim` of any namespace config for `k8s-metadata-injector` as shown in `deployment/cm.yaml`

The reason to supports these kind of resources is their importance to cost and usage estimations such that:
* pod will correspond to cpu/mem usages on AWS EC2s
* services will be correspond to AWS Load balancers
* persistentvolumeclaims will correspond to EBS volumes

The ability to automatically inject labels and annotations to the previous resources based on namespaces will simplify the classification and grouping of cluster resources that corresponds to AWS resources. It could be used with other tools for cost estimations like Cloudhealth.

To exclude resources from metadata injection you can use the following annotation:

```yaml
k8s-metadata-injector.kubernetes.io/skip": "true"
```

**Note:** the `kube-system` and `kube-public` namespaces are excluded from injection.

## Installation

To install `k8s-metadata-injector`:
* Ensure that MutatingAdmissionWebhook admission controllers are enabled.
* Ensure that the admissionregistration.k8s.io/v1beta1 API is enabled.
* For AWS, if tagging EBS volumes is needed, then `ebs-tagging` should be `true` in containers command line arguments. 

Then modify the config in `metadataconfig.yaml` as desired to inject the annotations and labels to all defined namespaces, and deploy:

```bash
kubectl apply -k install
```

### Namespace Configuration (`metadataconfig.yaml`):

* For version `2.0.0` and later, the metadata is configured by namespace. Further you can configure default values for all namespaces (even if not configured) by using `"*"`. For configured namespaces, the default values will be merged with namespace configuration such that namespaces will always override the default values. For example:

```yaml
namespaces:
    "*":
        pod:
        ...
        service:
        ...
        persistentVolumeClaim:
        ...
    default:
        pod:
            annotations:
                key: value
                ...
            lables:
                key: value
                ...
        service:
            annotations:
                key: value
                ...
            lables:
                key: value
                ...
        ...
    other_namespace:
        ...
    ...
```

For version `1.x.x`, the metadata is configured by resource types. For example

```yaml
pod:
    default:
        annotations:
            key: value
            ...
        lables:
            key: value
            ...
    other_namespace:
        ...
service:
    default:
        annotations:
            key: value
            ...
        lables:
            key: value
            ...          
    other_namespace:
        ...
...
```

### Required IAM policy:
To tag EBS volumes based on `ebs-tagger.kubernetes.io/ebs-additional-resource-tags` annotation, the following policy is needed:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": [
                "ec2:CreateTags"
            ],
            "Resource": "arn:aws:ec2:*:*:volume/*",
            "Effect": "Allow"
        }
    ]
}
```

In `deployment/install.yaml` kube2iam annotation `iam.amazonaws.com/role: KubernetesEBSCreateTagsAccess` is used as an example to allow `ebs-tagger` (small controller in k8s-metadata-injector) to tag EBS volumes.

## Build

To build `k8s-metadata-injector` as a docker container:

```bash
docker build -t abdullahalmariah/k8s-metadata-injector:latest .
docker push abdullahalmariah/k8s-metadata-injector:latest
```

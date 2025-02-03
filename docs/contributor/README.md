# KIM Snatch Architecture

Snatch is part or the Kyma Infrastructure Manager (KIM) and responsible to assign Kyma workloads to the Kyma owned worker pool. This document describes it's architecture and relevant technical flows.

![Snatch Architecture](./assets/architecture-webook.svg)


1) Gardener cert-manager issues a CA, TLS key and certificate. It stores them as entries in a Kubernetes Secret.
2) The KIM Snatch webhook mounts the Secret to make the CA, TLS key and certificate accessible through the local filesystem.
3) An webserver server ist started which exposes a HTTPS endpoint, using the mounted TLS certificate for securing incoming connections.
4) Changes of certificates are detected by a certificate watching library (Cert-Watcher).
5) Cert Watcher triggers a reload the HTTPS server configuration. This keeps the used TLS certificates up-to-date even after they were rotated.
6) Also the CA, used for creating the TLS certificates, is updated in the `WebhookConfiguration` by the Cert Watcher .
7) The `WebhookConfiguration` informs the Kubernetes API server about the existence of the webhook and refers to the CA. The API server uses CA to verify HTTPS connections established to the webhook.
8) API Server calls this HTTPS endpoint of the webhook for each received HTTP request. The webhook is allowed to modify the request before it's finally processed by the API server.


## Components

|Component|Purpose
|--|--|
|[Gardener cert-manager](https://github.com/gardener/cert-management)|Kubernetes Operator responsible for issuing and rotating TLS certificates.|
|[Issuer CR](https://github.com/gardener/cert-management?tab=readme-ov-file#setting-up-issuers)|Custom resource (CR) used by Gardener cert-manager for managing certificates and CA.|
|[Certificate CR](https://github.com/gardener/cert-management?tab=readme-ov-file#requesting-a-certificate)|CR used by Gardener cert-manager for issuing certificates.|
|Kubernetes Secret|Created by Gardener cert-manager to store the generated CA, TLS key and certificate.|
|WebhookConfiguration|Kubernetes CR considered by the API server to invoke webhooks during request processing.|
|API Server|The Kubernetes API server that processes HTTP requests and applies resource changes in ETCD.|
|Webhook|Webserver intercepting requests before the API server processes them.|
|Webhook HTTPS-Server|Invoked by the API Server; intercepts all incoming requests of the API server.|
|Webhook certs volume|Mount point that makes the data entries of the Secret accessible over the webhook's filesystem.|
|Webhook Cert Watcher|Monitoring changes applied on the mounted Secret.|

## How It Works

The activity diagram describes the internal technical process of Snatch more fine-grained:

![Flow](./assets/flow-snatch.svg)

# Mutating Webhook

The webhook is implemented as a [mutating webhook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#mutatingadmissionwebhook). Its goal is to inject a `nodeAffinity` to each Pod scheduled for a Kyma-managed namespace. The `nodeAffinity` is considered by the Kubernetes scheduler, which tries to schedule Pods only on the worker pool owned by Kyma. Worker pools created by customers are avoided.

## Non-Intrusive Behavior
To avoid any risk of interrupting workloads, the webhook is implemented defensively:

1. During the bootstrap, it verifies if at least one worker node is assigned to the worker pool owned by Kyma. If no matching worker node exists (which means the cluster has no Kyma-owned worker pool), the webhook logs an error and does not intercept any request.
2. The namespace of a Pod must be labeled with `operator.kyma-project.io/managed-by: kyma`. Only if this is the case does the Pod get a `nodeAffinity` assigned.
3. The assigned `nodeAffinity` is of type `preferredDuringSchedulingIgnoredDuringExecution`. The scheduler follows this affinity with the best effort but does not enforce it. This minimizes the risk that Pods aren't scheduled if a suitable node cannot be found. The node affinity is added with the  [**weight**](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#preferredschedulingterm-v1-core) of `10`, which allows the definition of further `nodeAffinity` rules with higher relevance. This value indicates the priority of this `nodeAffinity`, and ranges from 0=low to 100=high. 
4. Already scheduled Pods are not considered by the webhook. It only interferes with Pods not yet passed to the Kubernetes scheduler. Running Pods aren't touched (no `nodeAffinity` is added to their manifest) and are not restarted.

See an example of a Pod manifest with `nodeAffinity`, which was intercepted and updated by the mutating webhook:

    apiVersion: v1
    kind: Pod
    ...
    spec:
        affinity:
            nodeAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
            - preference:
                matchExpressions:
                - key: worker.gardener.cloud/pool
                    operator: In
                    values:
                    - cpu-worker-0
                weight: 10
    ...


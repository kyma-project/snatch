# Architecture

![Snatch Architecture](./assets/architecture-webook.svg)

## Components

|Component|Purpose
|--|--|
|[Gardener Cert Manager](https://github.com/gardener/cert-management)|Kubernetes Operator responsible for issuing and rotating TLS certificates.|
|[Issuer CR](https://github.com/gardener/cert-management?tab=readme-ov-file#setting-up-issuers)|Custom resource used by Gardener Cert Manager for managing certificates and CA.|
|[Certificate CR](https://github.com/gardener/cert-management?tab=readme-ov-file#requesting-a-certificate)|Custom resource used by Gardener Cert Manager for issuing a certificates.|
|Kubernetes Secret|Created by Gardener Cert-Manager to store the generated CA, TLS key and certificate.|
|WebhookConfiguration|Customer Resource of Kubernetes considered by API server to invoke webhooks during request processing.|
|API Server|API server of Kubernetes which processes HTTP request and applies resources changes in ETCD.|
|Webhook|Webserver intercepting requests before the API server is processing them.|
|Webhook HTTPS-Server|Invoked by the API Server and intercepts all incoming requests of the API server.|
|Webhook certs volume|Mount point which makes the data entries of the Secret accessible over the webhook's filesystem.|
|Webhook Cert Watcher|Monitoring changes applied on mounted secret.|

## How it works

* Gardener Cert Manager issues a CA, TLS key and certificate. It stores them as entires in a Kubernetes secret.
* A webhook is started (as pod).
   - It mounts the secret to make the CA, TLS key and certificate accessible via the local filesystem.
   - An HTTPS endpoint is exposed, using the mounted TLS certificate for securing incoming connections.
   - A Cert Watcher triggers an update of the `WebhookConfiguration` and a reload of the HTTPS server whenever an entry in the mounted secret was modified. This keeps the used secrets up-to-date even after they were rotated.
* The `WebhookConfiguration` informs the Kubernetes API server about the existence of the webhook and refers to the CA. The CA will be used by the API server to verify  HTTPS connections established to the webhook.
* API Server calls this HTTPS endpoint of the webhook for each received HTTP request. The webhook is allowed to modify the  request before it's finally processed by the API server.

## High Level Flow

![Flow](./assets/flow-snatch.svg)

# Mutating Webhook

The webhook is implemented as an [mutating webhook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#mutatingadmissionwebhook), as its goal is to inject a `nodeAffinity` to each pod which is scheduled for an Kyma managed namespace. The `nodeAffinity` is considered by the Kubernetes scheduler which tries to schedule pods only on the  worker pool owned by Kyma. Worker pools created by customers should be avoided.

## Non-intrusive behavior
To avoid any risk of interrupting workloads, the webhook is implemented defensively:

1. During the bootstrap, it verifies if at least one worker node is assigned to the worker pool owned by Kyma. If no matching worker node exists (means, the cluster has no Kyma owned worker pool), the webhook will log an error and not intercept any request.
2. The namespace of a pod has to be labeled with `operator.kyma-project.io/managed-by: kyma`. Only if this is the case, the pod will get a `nodeAffinity` assigned.
3. The assigned `nodeAffinity` is of type `preferredDuringSchedulingIgnoredDuringExecution` . The scheduler follows this affinity with best effort, but not enforcing it. This minimizes the risk that pods won't be scheduled if a suitable node cannot be found. The node affinity is added with a  [`weight`](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#preferredschedulingterm-v1-core) of 10 (indicates the priority of this `nodeAffinity`, 0=low, 100=high) which allows the definition of further `nodeAffinity` rules with higher relevance.
4. Already scheduled pods are not considered by the webhook. It interferes only pods which are not yet passed to the Kubernetes scheduler. Means, running pods won't be touched (no `nodeAffinity` will be added to their manifest) and also not restarted.

## Example pod manifest with `nodeAffinity`
This is an example of pod manifest which was intercepted and updated by the mutating webhook:

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


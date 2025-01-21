# KIM Snatch

## Overview
The KIM-Snatch Module is part of KIM's worker-pool feature. It is a mandatory Kyma module and deployed on all Kyma managed runtimes (SKR). 

In the past, Kyma had only one worker-pool (so called "Kyma worker-pool") where every workload was scheduled on. This Kyma worker pool is mandatory and cannot be removed from a Kyma runtime. Customers have several configuration options, but it's not fully adjustable and can be too limited for customers who require special node setups.

By introducing the Kyma worker-pool feature, customers can add additional worker-pools to their Kyma runtime. This enables customer to introduce worker nodes, which are optimized for their particular workload requirements. 

 To ensure customer worker-pools are reserved for customer workloads, KIM-Snatch got introduced. It is responsible to assign Kyma workloads (e.g. operators of Kyma modules) to the Kyma worker pool. This has several advantages:

* Kyma workloads are not allocating resources on customer worker-pools. This ensures that customers have the full capacity of the worker-pool available for their workloads.
* It reduce the risk of incompatibility between Kyma container images and individually configured worker-pools.

## Technical Approach
The KIM-Snatch module introduced a [mutating admission webhook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#mutatingadmissionwebhook) in Kubernetes.

It is intercepting all pods which are scheduled in a Kyma managed namespace.  A managed namespace is by [KLM](https://github.com/kyma-project/lifecycle-manager) always labeled with `operator.kyma-project.io/managed-by: kyma`. KIM reacts only on pods which are scheduled in one of these labeled namespaces. Typical Kyma managed namespaces are `kyma-system` or, if the Kyma Istio module is used,  `istio`.

Before the pod is handed over to the Kubernetes scheduler, KIM-Snatch adds a [`node affinity`](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity) to it. This tells the scheduler, to prefer the Kyma worker-pool nodes for this pod. 

## Limitations

### Kyma worker-pool is not enforced
Assigning a pod to a particular worker pool can cause drawbacks,  for example:

* Resources of the preferred worker-pool are exhausted while other worker-pools would have still free capacities.
* If no suitable worker-pool can be found and the node-affiniuty is set as a "hard" rule, the pod won't be scheduled.

To overcome these limitations, the configured node-affinity on Kyma workloads is a "soft" rule. It tells the Kubernetes scheduler to prefer the Kyma worker-pool, but if no capacities are available, it will also consider other worker-pools.

### Not all Kyma workloads intercepted
Another disadvantage is, that Kubernetes calls could be heavily impacted, if a mandatory admission webhook isn't responsive enough. This can lead to timeouts and massive performance degradation.

To prevent such side-effects, the webhook is configured as non-mandatory webhook which allows Kubernetes to bypass it. This could, while the webhook is down, lead to scheduled pods without a `nodeAffinity` configured.

Additionally, all pods which are already scheduled and running on a worker node won't receive the `nodeAffinity` as it's only allowed to intercept non-scheduled pods. Means, running pods would have to be restarted to receive the `nodeAffinity`. This webhook is not restarting running pods to avoid any service interruptions or reduced user experience for our customers.
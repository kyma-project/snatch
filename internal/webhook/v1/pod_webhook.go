/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	kymaNodeSelectorKey = "worker.gardener.cloud/pool"
)

// nolint:unused
// log is for logging in this package.
var podlog = logf.Log.WithName("pod-resource")

type defaultPod = func(*corev1.Pod)

// SetupPodWebhookWithManager registers the webhook for Pod in the manager.
func SetupPodWebhookWithManager(mgr ctrl.Manager, defdefaultPod defaultPod) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Pod{}).
		WithDefaulter(&PodCustomDefaulter{defaultPod: defdefaultPod}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mpod-v1.kb.io,admissionReviewVersions=v1,matchPolicy=Exact,reinvocationPolicy=Never

//+kubebuilder:rbac:groups="",resources=nodes,verbs=list
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;patch

// PodCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Pod when those are created or updated.
type PodCustomDefaulter struct {
	defaultPod func(*corev1.Pod)
}

var _ webhook.CustomDefaulter = &PodCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Pod.
func (d *PodCustomDefaulter) Default(ctx context.Context, obj runtime.Object) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			// no panice
			return
		}

		switch x := r.(type) {
		case string:
			err = fmt.Errorf("%s", x)
		case error:
			err = x
		default:
			err = fmt.Errorf("unknown defaulting function panic: %s", r)
		}
	}()

	pod, ok := obj.(*corev1.Pod)

	if !ok {
		return fmt.Errorf("expected an Pod object but got %T", obj)
	}

	podlog.Info(
		"injecting node affinity",
		"name", pod.GetName(),
		"ns", pod.GetNamespace(),
		"uuid", pod.GetUID(),
		"labels", pod.GetLabels(),
	)
	d.defaultPod(pod)
	return nil
}

func ApplyDefaults(nodeSelectorValue string, omittedNamespaces []string) defaultPod {
	return func(pod *corev1.Pod) {
		if slices.Contains(omittedNamespaces, pod.Namespace) {
			podlog.Info("omitting affinity injection: forbidden namespace", "name", pod.Namespace)
			return
		}

		if pod.Spec.Affinity == nil {
			pod.Spec.Affinity = &corev1.Affinity{}
		}

		if pod.Spec.Affinity.NodeAffinity == nil {
			pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
		}

		if pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
			pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution =
				[]corev1.PreferredSchedulingTerm{}
		}

		pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution =
			append(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
				corev1.PreferredSchedulingTerm{
					Weight: 10,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      kymaNodeSelectorKey,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{nodeSelectorValue},
							},
						},
					},
				})
	}
}

var ErrNodeNotFound = fmt.Errorf("node selector not found")

func ApplyDefaultsFallback(nodeSelectorValue string) defaultPod {
	return func(pod *corev1.Pod) {
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}

		pod.Annotations[kymaNodeSelectorKey] = nodeSelectorValue
		podlog.Error(ErrNodeNotFound, "unable to set node selector",
			"node-selector-value", nodeSelectorValue,
		)
	}
}

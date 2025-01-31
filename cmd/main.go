/* Copyright 2024.

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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/kyma-project/kim-snatch/internal/metricsa"
	"os"
	"path"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	corev1 "k8s.io/api/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	"github.com/kyma-project/kim-snatch/internal/webhook/callback"
	webhook "github.com/kyma-project/kim-snatch/internal/webhook/server"
	webhookcorev1 "github.com/kyma-project/kim-snatch/internal/webhook/v1"
	admissionregistration "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	// +kubebuilder:scaffold:imports
)

const (
	certDir                  = "/tmp/"
	certificateAuthorityName = "ca.crt"
	flagWebhookConfigName    = "webhook-cfg-name"
	flagKymaWorkerPoolName   = "kyma-worker-pool-name"
	patchFieldManagerName    = "snatch"
	webhookServerKeyName     = "tls.key"
	webhookServerCertName    = "tls.crt"
)

var (
	scheme             = runtime.NewScheme()
	logger             = ctrl.Log.WithName("setup")
	errInvalidArgument = fmt.Errorf("invalid argument")
	omittedNamespaces  = []string{"kube-system"}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(admissionregistration.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)

	var mWhCfgName string
	var kymaWorkerPoolName string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	// webhook flags
	flag.StringVar(&mWhCfgName, flagWebhookConfigName, "", "The name of the mutating webhook configuration to be updated.")
	flag.StringVar(&kymaWorkerPoolName, flagKymaWorkerPoolName, "", "The name of the workerpool the kyma components will be scheduled on.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// validate flags
	for _, pair := range [][2]string{
		{flagWebhookConfigName, mWhCfgName},
		{flagKymaWorkerPoolName, kymaWorkerPoolName},
	} {
		if pair[1] == "" {
			logger.Error(errInvalidArgument, pair[0], pair[0])
			os.Exit(1)
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		logger.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Error(err, "unable to create rest configuration")
		os.Exit(1)
	}

	rtClient, err := client.New(config, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		logger.Error(err, "unable to create client")
		os.Exit(1)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts:  tlsOpts,
		CertDir:  certDir,
		KeyName:  webhookServerKeyName,
		CertName: webhookServerCertName,
		//Metrics:  metrics.NewMetrics(),
		Callback: func(cert tls.Certificate) {
			// read regenerated certificate
			certPath := path.Join(certDir, certificateAuthorityName)
			data, err := os.ReadFile(certPath)
			if err != nil {
				logger.Error(err, "unable to read certificate")
				os.Exit(1)
			}
			logger.Info("certificate loaded")

			updateCABundle := callback.BuildUpdateCABundle(
				context.Background(),
				rtClient,
				callback.BuildUpdateCABundleOpts{
					Name:         mWhCfgName,
					CABundle:     data,
					FieldManager: patchFieldManagerName,
				})

			if err := retry.RetryOnConflict(retry.DefaultBackoff, updateCABundle); err != nil {
				logger.Error(err, "unable to patch mutating webhook configuration")
				os.Exit(1)
			}
		},
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress: metricsAddr,
		TLSOpts:     tlsOpts,
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			return rtClient, nil
		},
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}
	mtr := metricsa.NewMetrics()

	var nodeList corev1.NodeList
	if err := rtClient.List(context.TODO(), &nodeList, client.MatchingLabels{
		"worker.gardener.cloud/pool": kymaWorkerPoolName,
	}); err != nil {
		logger.Error(err, "unable to fetch node list")
		os.Exit(1)
	}

	defaultPod := webhookcorev1.ApplyDefaults(kymaWorkerPoolName, omittedNamespaces)
	if len(nodeList.Items) == 0 {
		errMsg := fmt.Sprintf("worker.gardener.cloud/pool=%s not exist, switching to fallback",
			kymaWorkerPoolName)
		mtr.SetFallbackShoot()
		logger.Error(errInvalidArgument, errMsg)
		defaultPod = webhookcorev1.ApplyDefaultsFallback(kymaWorkerPoolName)
	} else {
		mtr.SetDefaultShoot()
	}

	if err = webhookcorev1.SetupPodWebhookWithManager(mgr, defaultPod); err != nil {
		logger.Error(err, "unable to create webhook", "webhook", "Pod")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		os.Exit(1)
	}
}

package callback

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	admissionregistration "k8s.io/api/admissionregistration/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BuildUpdateCABundleOpts struct {
	// Name of the the mutating webhook configuration to be updated
	Name string
	// CABundle the mutating webhook configuration webhooks will be updated with
	CABundle []byte
	// FiledManager the name of the filed manager for patch operation
	FieldManager string
}

// buildUpdateCABundle - builds a function that will update certificate authority
func BuildUpdateCABundle(
	ctx context.Context,
	rtClient client.Client,
	opts BuildUpdateCABundleOpts) func() error {

	logger := slog.Default()
	return func() error {
		getCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		var mWhCfg admissionregistration.MutatingWebhookConfiguration
		if err := rtClient.Get(
			getCtx,
			client.ObjectKey{Name: opts.Name},
			&mWhCfg); err != nil {
			return fmt.Errorf("unable to get mutating webhook configuration: %w", err)
		}

		var updated bool
		for i := 0; i < len(mWhCfg.Webhooks); i++ {
			if bytes.Equal(opts.CABundle, mWhCfg.Webhooks[i].ClientConfig.CABundle) {
				continue
			}
			mWhCfg.Webhooks[i].ClientConfig.CABundle = opts.CABundle
			updated = true
		}

		if !updated {
			logger.Info("mutating webhook configuration up to date")
			return nil
		}

		mWhCfg.Kind = "MutatingWebhookConfiguration"
		mWhCfg.APIVersion = "admissionregistration.k8s.io/v1"
		mWhCfg.ManagedFields = nil

		patchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		logger.Info("attempting to patch mutating webhook configuration", "name", mWhCfg.Name)

		return rtClient.Patch(patchCtx, &mWhCfg, client.Apply, &client.PatchOptions{
			FieldManager: opts.FieldManager,
			Force:        ptr.To(true),
		})
	}
}

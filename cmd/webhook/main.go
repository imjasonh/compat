package main

import (
	"context"

	"github.com/GoogleCloudPlatform/compat/pkg/apis"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/webhook"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/resourcesemantics"
)

const (
	// webhookLogKey is the name of the logger for the webhook cmd
	webhookLogKey = "webhook"
	configName    = "config-logging"
)

func NewResourceAdmissionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	ctxFunc := func(ctx context.Context) context.Context {
		// return v1.WithUpgradeViaDefaulting(store.ToContext(ctx))
		return ctx
	}

	return resourcesemantics.NewAdmissionController(ctx,
		// Name of the resource webhook.
		"webhook.cloudbuild.googleapis.com",

		// The path on which to serve the webhook.
		"/",

		// The resources to validate and default.
		map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
			apis.BuildGVK: &apis.Build{},
		},

		// A function that infuses the context passed to Validate/SetDefaults with custom metadata.
		ctxFunc,

		// Whether to disallow unknown fields.
		true,
	)
}

func main() {
	// Set up a signal context with our webhook options
	ctx := webhook.WithOptions(signals.NewContext(), webhook.Options{
		ServiceName: "webhook",
		Port:        8443,
		SecretName:  "webhook-certs",
	})

	sharedmain.MainWithContext(ctx, "webhook",
		certificates.NewController,
		NewResourceAdmissionController,
	)
}

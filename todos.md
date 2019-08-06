Notes on supporting some NYI features...

# HTTPS

User must own an verify a domain, which they need to define in an
otherwise-predefined K8s Ingress, with a ManagedCertificate.

* https://cloud.google.com/kubernetes-engine/docs/concepts/ingress
* https://cloud.google.com/kubernetes-engine/docs/how-to/managed-certs

User can modify the installation to use a non-Google-managed SSL certificate if
they want.

This will be a security requirement, but adds a constant monthly cost to using
the service.

# Encrypted Env Vars

Decrypt the values in the API server (authorized as the GSA) and create a K8s
Secret with that value (named after sha256 of content bytes?), with
`EnvFrom.SecretRef.Name`.

The user should configure [Application-layer Secrets
Encryption](https://cloud.google.com/kubernetes-engine/docs/how-to/encrypting-secrets)
on the cluster to encrypt values using a KMS key.

# Cached container images

Allow the user to define some container images which should always be available
on every node before it reports itself as ready for workloads.

Use [warm-image](https://github.com/mattmoor/warm-image) which does this using a
`DaemonSet`.

The list could be exposed as a ConfigMap or even an HTTP endpoint which
creates/deletes `WarmImage` resources.

# Step Timeouts

For each step, set a `ReadinessProbe`
([`Probe`](https://godoc.org/k8s.io/api/core/v1#Probe)) with
`InitialDelaySeconds` of the timeout duration and `Handler.Exec` of a command
that doesn't exist or fails with a known status code or error message. The
Service will check for this failure state to determine if the failure means a
step timeout.

Possibly not doable since all containers start at once and they just wait to be
started. It's possible we could update each container to have a readiness probe
only after we've seen it's started, but we'd have to test it. I'm not sure how
important it is to support step-level timeouts if it's going to be this kludgey.
Or maybe it's worth just supporting it Tekton directly.

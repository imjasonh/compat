# GCB Compatibility for Tekton

**This is an experimental work in progress**

This project provides an API service that can drive a
[Tekton](https://tekton.dev)-enabled GKE cluster to emulate the [Google Cloud
Build](https://cloud.google.com/cloud-build) service. The aim is to support the
full [`gcloud builds
...`](https://cloud.google.com/sdk/gcloud/reference/builds/) CLI surface, with
some useful [new features](#new-features), but with some necessary
[limitations](#limitations), [incompatibilitiies](#incompatibilities) and
assorted [differences](#differences).

## TODOs

A partial list:

- [ ] Support .tgz source archives uploaded from gcloud
- [ ] Streaming logs to GCS
- [ ] Resolve GCS source provenance at Build creation time
- [ ] Support container image outputs, report built image digests
- [ ] Report build step image digests
- [ ] CancelBuild
- [ ] Real versioned release YAMLs

### Differences

* Builds are authorized by an new IAM service account
  (`gcb-compat@[PROJECT_ID].iam.gserviceaccount.com`), not the usual GCB
  builder service account (`[PROJECT_NUMBER]@cloudbuild.gserviceaccount.com`)
* Because builds are translated to Tekton `TaskRun`s and executed on the
  cluster, any user with permission to delete `TaskRun`s on the cluster can
  modify build history.
* By default, builds don't specify a disk resource request, and so are given
  whatever default disk resources are available on the node. To specify disk
  resource needs, specify `diskSizeGb`.
* The project will be billed for GKE cluster usage while the cluster exists, and
  not on a per-build-minute basis as Cloud Build does today. Bin-packing and
  autoscaling can help lower these costs.

### Limitations

* The service is only intended to run on GKE.
* Builds cannot access the Docker socket, e.g., to run `docker build` or
  `docker run`.
* Builds can only be requested for the project where the cluster itself is
  running at this time.
* Users cannot override the `logsBucket` at this time -- logs will always be
  written to the same bucket used by gcloud to stage source code
  (`gs://[PROJECT_ID]_cloudbuild/`)
* Only GCS source is supported at this time.
* Lines in build logs are not prefixed with the step number at this time.
* Substitutions are not supported at this time.

### Incompatibilities

* Some step features are unsupported: `waitFor` and `id`, `secretEnv`, and
  step-level `timeout`.

### New Features

Because builds execute on a GKE cluster, a number of things are now possible,
including:

* Access to resources on the cluster's [private VPC
  network](https://cloud.google.com/kubernetes-engine/docs/how-to/cluster-shared-vpc).
* The cluster can be configured to [only be visible to authorized VPC
  networks](https://cloud.google.com/kubernetes-engine/docs/how-to/private-clusters).
* Builds share VM node resources ("bin-packing") for more effective resource
  use. This also has benefits to builder image pull latency, since some images
  may already be available from previous builds.
* Nodes can be configured for
  [autoscaling](https://cloud.google.com/kubernetes-engine/docs/concepts/cluster-autoscaler).
* Builds are run as Pods on the cluster, and export resource usage metrics (CPU,
  RAM, etc.) to [Stackdriver
  Monitoring](https://cloud.google.com/monitoring/kubernetes-engine/).
* Authorized users can delete items from build history, which can be useful in
  some cases, for instance credential leaks.

### Supported features

* Nearly-complete GCB API compatibility: builds can be created, listed, etc.
* Except for [incompatibilities](#incompatibilities) above, all `steps` features
  are supported.
* Log streaming to GCS (_currently not streaming_)
* API authorization: users cannot request builds without permission
* Builder service accoount auth: builds can access GCP resources as
  `gcb-compat@[PROJECT_ID].iam.gserviceaccount.com`
* Cross-step volume mounts
* `machineType` and `diskSizeGb` are translated into Kubernetes resource
  requests -- if the cluster's nodes have insufficient available resources,
  builds will queue. Consider enabling GKE's [node
  auto-provisioning](https://cloud.google.com/kubernetes-engine/docs/how-to/node-auto-provisioning)
  to automatically create nodes of the correct size to handle these builds.

## Setup

There are two options for installing the Service and connecting it to your
cluster resources:

* [Install on GKE](docs/install_cluster.md)
* [Install on Cloud Run](docs/install_cloud_run.md)

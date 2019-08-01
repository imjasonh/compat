# GCB Compatibility for Tekton

**This is an experimental work in progress**

This project provides a Kubernetes service that can be installed on a
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
- [ ] Generate LogUrl
- [ ] CancelBuild

### Differences

* Builds are authorized by an new IAM service account
  (`gcb-compat@[PROJECT_ID].iam.gserviceaccount.com`), not the usual GCB
  builder service account (`[PROJECT_NUMBER]@cloudbuild.gserviceaccount.com`)
* Because builds are translated to Tekton `TaskRun`s and executed on the
  cluster, any user with permission to delete `TaskRun`s on the cluster can
  modify build history.

### Limitations

* The compatibility service only runs on GKE.
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
* `machineType` and `diskSizeGb` are unsupported at this time.

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
  use, and nodes can be configured for
  [autoscaling](https://cloud.google.com/kubernetes-engine/docs/concepts/cluster-autoscaler).

### Supported features

* Nearly-complete GCB API compatibility: builds can be created, listed, etc.
* Except for [incompatibilities](#incompatibilities) above, all `steps` features
  are supported.
* Log streaming to GCS (_currently not streaming_)
* API authorization: users cannot request builds without permission
* Builder service accoount auth: builds can access GCP resources as
  `gcb-compat@[PROJECT_ID].iam.gserviceaccount.com`
* Cross-step volume mounts

## Setup

Prerequisites:

1. A GKE cluster with [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) enabled.
1. A working Tekton installation on the cluster.

With those prerequisites satisfied, install the GCB compatibility service:

```
KO_DOCKER_REPO=gcr.io/my-gcp-project
ko apply -f config/
```

**TODO: Real versioned release YAMLs**

This builds and deploys the replicated Kubernetes Service behind a Load
Balancer, all in the namespace `gcb-compat`, running as the Kubernetes Service
Account `gcb-compat-account`.

Next, set up Workload Identity:

```
PROJECT_ID=$(gcloud config get-value project)
gcloud iam service-accounts create gcb-compat
gcloud iam service-accounts add-iam-policy-binding \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:${PROJECT_ID}.svc.id.goog[gcb-compat/gcb-compat-account]" \
  gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com
kubectl annotate serviceaccount \
  --namespace gcb-compat \
  gcb-compat-account \
  iam.gke.io/gcp-service-account=gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member serviceAccount:gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/storage.storageAdmin
```

This creates a GCP Service Account ("GSA") and grants the `gcb-compat-account`
Kubernetes Service Account (KSA) permission to act as that GSA.

At this point, you can grant any desired GCP IAM roles to the service account.
For instance, to give the GSA permission to view GCB builds:

```
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member serviceAccount:gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/cloudbuild.builds.viewer
```

## Testing

First, get the address of the load balancer created above, and tell `gcloud` to
use that service instead of the regular GCB API service:

```
SERVICE_IP=$(kubectl get service gcb-compat-service -n gcb-compat -ojsonpath="{.status.loadBalancer.ingress[0].ip})"
export CLOUDSDK_API_ENDPOINT_OVERRIDES_CLOUDBUILD=http://${SERVICE_IP}/
```

**NB:** It might take a minute or two for the Service to get its IP right after
you create it.

Now we'll tell `gcloud` to run a simple build:

```
cat > cloudbuild.yaml << EOF
steps:
- name: ubuntu
  args: ['echo', 'hello']
EOF
gcloud builds submit --no-source
```

This currently doesn't stream logs (😅), but the build started!

```
$ gcloud builds list
ID                                    CREATE_TIME  DURATION  SOURCE  IMAGES  STATUS
6b0c5eea-f06d-4e5b-998a-76d0e4941376  -            28S       -       -       SUCCESS
c13efb20-cc33-4c4e-b605-2595cca63791  -            4S        -       -       WORKING # <--- yeessss
```

It's working! Let's see if it succeeds:

```
$ gcloud builds describe c13efb20-cc33-4c4e-b605-2595cca63791
finishTime: '2019-07-30T20:50:39Z'
id: c13efb20-cc33-4c4e-b605-2595cca63791
results: {}
startTime: '2019-07-30T20:50:35Z'
status: SUCCESS
steps:
- args:
  - go
  - version
  name: golang
  status: SUCCESS
  timing:
    endTime: '2019-07-30T20:50:39Z'
    startTime: '2019-07-30T20:50:38Z'
```

🎉🎉🎉


## Cleaning up

To tear down just the Service running on the cluster:

```
kubectl delete -f config/
```

To delete the IAM Service Account:

```
gcloud iam service-accounts delete gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com
```

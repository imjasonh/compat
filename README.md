# GCB Compatibility for Tekton

This is a work in progress

## TODOs

A partial list:

- [ ] Authorizing API requests
- [ ] Streaming logs to GCS
- [ ] Builder Service Account auth, using Workload Identity
- [ ] Resolve GCS source provenance at Build creation time
- [ ] Support container image outputs, report built image digests
- [ ] Report build step image digests

## Setup

Prerequisites:

1. A GKE cluster with [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) enabled.
1. A working Tekton installation on the cluster.

With those prerequisites satisfied, install the GCB compatibility service:

```
KO_DOCKER_REPO=gcr.io/my-gcp-project
ko apply -f config/
```

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

Now we'll tell `gcloud` to run a simple build:

```
cat > cloudbuild.yaml << EOF
steps:
- name: ubuntu
  args: ['echo', 'hello']
EOF
gcloud builds submit --no-source
```

This currently fails with a panic in `gcloud` (😅), but the build started!

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

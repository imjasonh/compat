# Installing on GKE

This doc describes installing the GCB Compatibility Service on your own GKE
cluster. This might be preferrable for you if you already have experience with
securely setting up services on Kubernetes and GKE, and if you have security
requirements that dictate that services run on a GKE cluster.

When installed this way, the service runs as a K8s Deployment and Service on
nodes inside your cluster.

![Diagram of cluster installation](./cluster.png)

The other alternative is to run the Service on Cloud Run, which gives you
automatic autoscaling to zero, usage-based billing, and free managed SSL
certificates. Installation instructions for Cloud Run are
[here](install_cloud_run.md).

## Installation

Prerequisites:

* A GKE cluster with [Workload
  Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)
  enabled and [Tekton
  installed](https://github.com/tektoncd/pipeline/blob/master/docs/install.md).

If you don't already have a cluster, this will create one and install the latest
Tekton release:

```
PROJECT_ID=$(gcloud config get-value project)
ZONE=us-east4-a
gcloud beta container clusters create new-cluster --zone=${ZONE} \
  --machine-type=n1-standard-4 --num-nodes=3 \
  --identity-namespace=${PROJECT_ID}.svc.id.goog
gcloud container clusters get-credentials new-cluster --zone=${ZONE}
kubectl apply -f https://storage.googleapis.com/tekton-releases/latest/release.yaml
```

With those prerequisites satisfied, create the Kubernetes service account:

```
kubectl apply -f config/100-serviceaccount.yaml
```

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
  --role roles/storage.admin
```

This creates a GCP Service Account ("GSA") and grants the `gcb-compat-account`
Kubernetes Service Account (KSA) permission to act as that GSA.

Now, install the full service:

```
KO_DOCKER_REPO=gcr.io/${PROJECT_ID}
ko apply -f config/
```

This builds and deploys the replicated Kubernetes Service behind a Load
Balancer, all in the namespace `gcb-compat`, running as the Kubernetes Service
Account `gcb-compat-account`.

At this point, you can grant any desired GCP IAM roles to the service account.
For instance, to give the GSA permission to deploy to App Engine:

```
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member serviceAccount:gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/appengine.deployer
```

## Testing

First, get the address of the load balancer created above, and tell `gcloud` to
use that service instead of the regular GCB API service:

```
SERVICE_IP=$(kubectl get service gcb-compat-service -n gcb-compat -ojsonpath="{.status.loadBalancer.ingress[0].ip}")
gcloud config set api_endpoint_overrides/cloudbuild http://${SERVICE_IP}/
```

**NB:** It might take a minute or two for the Service to get its IP right after
you create it.

**Important:** This configuration will send your build request and Google
credentials _in the clear_ over HTTP. You should set up HTTPS for this service
using Ingress and SSL certs (TODO: document this).

Now that we've pointed `gcloud` at the Service deployed on your cluster,
we'll tell `gcloud` to run a simple build:

```
cat > cloudbuild.yaml << EOF
steps:
- name: ubuntu
  args: ['echo', 'hello']
EOF
gcloud builds submit --no-source
```

The build has started! You should see logs streamed to your console, until the
build completes:

```
Created [http://XX.XXX.XXX.XXX/v1/projects/my-project/builds/79deb463-6a02-4f65-aae5-c572095d7835].
Logs are available in the Cloud Console.
----------------------------------- REMOTE BUILD OUTPUT ----------------------------------
hello
------------------------------------------------------------------------------------------
ID                                    CREATE_TIME                DURATION  SOURCE  IMAGES  STATUS
79deb463-6a02-4f65-aae5-c572095d7835  2019-08-14T16:52:08+00:00  6S        -       -       SUCCESS
```

Let's get the build details:

```
$ gcloud builds describe 79deb463-6a02-4f65-aae5-c572095d7835
createTime: '2019-08-14T16:52:08Z'
finishTime: '2019-08-14T16:52:14Z'
id: 79deb463-6a02-4f65-aae5-c572095d7835
logsBucket: gs://my-project_cloudbuild
results: {}
startTime: '2019-08-14T16:52:08Z'
status: SUCCESS
steps:
- args:
  - echo
  - hello
  name: ubuntu
  status: SUCCESS
  timing:
    endTime: '2019-08-14T16:52:13Z'
    startTime: '2019-08-14T16:52:11Z'
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

And to point `gcloud` at the standard hosted GCB API:

```
gcloud config unset api_endpoint_overrides/cloudbuild
```

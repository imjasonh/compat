# Installing on Cloud Run

This doc describes installing the GCB Compatibility Service on [Cloud
Run](https://cloud.google.com/run). This might be preferrable if you don't have
much experience with Kubernetes or GKE, and if you want simpler deployment and
management of the service, including autoscaling and Google-managed SSL
certificates.

When installed this way, the service runs in Cloud Run outside your cluster and connects to the cluster to run workloads.

[!./cloud_run.png]

The other alternative is to run the Service on your GKE cluster itself, which
gives you more control over the security and accessibility of the Service, but
requires more care to securely set it up. Installation instructions for GKE are
[here](install_cluster.md).

## Installation

In order to run off-cluster, the builder service account (`gcb-compat@`) has to
have permission to view clusters in the project:

```
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member serviceAccount:gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/container.clusterViewer
```

The builder service account also has to have permissions to create and manage
Tekton resources on the cluster:

```
kubectl create rolebinding off-cluster-binding \
  --role=gcb-compat-role \
  --user=gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com -n gcb-compat
```

Now, create the Cloud Run service, pointing at the cluster on which you want it
to run builds:

```
PROJECT_ID=$(gcloud config get-value project)
CLUSTER_ZONE=us-east4-a
CLUSTER_NAME=my-cluster-name
IMAGE=gcr.io/${PROJECT_ID}/github.com/ImJasonH/compat

gcloud beta run deploy gcb-compat \
  --platform=managed \
  --allow-unauthenticated \
  --service-account=gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --image=${IMAGE} \
  --set-env-vars=CLUSTER_NAME=projects/${PROJECT_ID}/locations/${CLUSTER_ZONE}/clusters/${CLUSTER_NAME}
```

NB: Cloud Run services can only run in a handful of regions, so the region in
which the compat service runs might be different from the region where the
target cluster's. If possible, try to run them in the same region to reduce the
likelihood and impact of outages.

## Testing

When deploying succeeds, `gcloud` will print the URL where the new service is
running:

```
Service [gcb-compat] revision [gcb-compat-12345] has been deployed and is serving traffic at https://gcb-compat-blahblahblah.a.run.app
```

Next, tell `gcloud` to use that service instead of the regular GCB API service:

```
gcloud config set api_endpoint_overrides/cloudbuild https://gcb-compat-blahblahblah.a.run.app/
```

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

Now we can get its logs:

```
$ gcloud builds log c13efb20-cc33-4c4e-b605-2595cca63791
----------------------------------- REMOTE BUILD OUTPUT ----------------------------------
hello
------------------------------------------------------------------------------------------
```

🎉🎉🎉


## Cleaning up

To tear down just the Service running on the cluster:

```
gcloud beta run services delete gcb-compat --platform=managed
```

To delete the IAM Service Account:

```
gcloud iam service-accounts delete gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com
```

And to point `gcloud` at the standard hosted GCB API:

```
gcloud config unset api_endpoint_overrides/cloudbuild
```

# Installing gcb-compat on Cloud Run

- TODO: why you'd want to do something like that, tradeoffs

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

When this succeeds, it will log the URL where the new service is running:

```
Service [gcb-compat] revision [gcb-compat-12345] has been deployed and is serving traffic at https://gcb-compat-blahblahblah.a.run.app
```

Point `gcloud` at this endpoint:

```
export CLOUDSDK_API_ENDPOINT_OVERRIDES_CLOUDBUILD=https://gcb-compat-blahblahblah.a.run.app/
```

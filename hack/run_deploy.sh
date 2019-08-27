#!/bin/bash

PROJECT_ID=$(gcloud config get-value project)
CLUSTER_ZONE=us-east4-a
CLUSTER_NAME=compat-cluster
IMAGE=$(KO_DOCKER_REPO=gcr.io/${PROJECT_ID} ko publish -P ./cmd/api)

echo built ${IMAGE}

gcloud beta run deploy gcb-compat \
  --platform=managed \
  --allow-unauthenticated \
  --service-account=gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --image=${IMAGE} \
  --set-env-vars=CLUSTER_NAME=projects/${PROJECT_ID}/locations/${CLUSTER_ZONE}/clusters/${CLUSTER_NAME}

addr=$(gcloud beta run services describe gcb-compat --platform=managed --format="value(status.address.hostname)")

gcloud config set api_endpoint_overrides/cloudbuild ${addr}/

# Sanity check
gcloud builds list

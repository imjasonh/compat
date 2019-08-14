# Publishing Build Updates to Cloud Pub/Sub

When builds are created, make progress or complete, the service will attempt to
publish to a Cloud Pub/Sub topic named `cloud-builds`, just like the hosted GCB
API does, as described in the docs:
https://cloud.google.com/cloud-build/docs/send-build-notifications

Messages are published on a best-effort basis. If the service account is not
authorized to publish to the topic, or if the topic doesn't exist, or if there
is any error publishing to the topic, the build's execution will not be
affected. If the service is restarted while a build is ongoing, future updates
will not be published to Cloud Pub/Sub.

By default, the service account is _not_ authorized to publish to the topic. To
authorize the service account to publish to the `cloud-builds` topic, first make
sure the topic exists:

```
gcloud beta pubsub topics create cloud-builds
```

Then, grant the **Publisher** role to the service account:

```
gcloud beta pubsub topics add-iam-policy-binding cloud-builds \
  --member serviceAccount:gcb-compat@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/pubsub.publisher
```

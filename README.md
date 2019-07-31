# GCB Compatibility for Tekton

This is a work in progress

## Deploying

```
KO_DOCKER_REPO=gcr.io/my-gcp-project
ko apply -f config/
```

This builds and deploys the replicated service behind a load balancer.

## Testing

First, get the address of the load balancer created above:

```
$ SERVICE_IP=$(kubectl get service gcb-compat-service -n gcb-compat -ojsonpath="{.status.loadBalancer.ingress[0].ip})"
```

Then, we'll tell `gcloud` to talk to that IP instead of the regular GCB API:

```
$ export CLOUDSDK_API_ENDPOINT_OVERRIDES_CLOUDBUILD=http://$SERVICE_IP/
```

Now we'll tell `gcloud` to run a simple build:

```
cat > cloudbuild.yaml << EOF
steps:
- name: golang
  args: ['go', 'version']
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

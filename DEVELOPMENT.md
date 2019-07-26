# Building the image

Using [`ko`](https://github.com/google/ko):

```
KO_DOCKER_REPO=gcr.io/my-gcp-project ko publish .
```

This will build and push an image to
`gcr.io/my-gcp-project/github.com/ImJasonH/compat` and print the image's
digest.

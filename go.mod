module github.com/GoogleCloudPlatform/compat

go 1.13

require (
	cloud.google.com/go v0.47.0
	cloud.google.com/go/pubsub v1.0.1
	github.com/google/uuid v1.1.1
	github.com/julienschmidt/httprouter v1.3.0
	github.com/mattbaird/jsonpatch v0.0.0-20171005235357-81af80346b1a // indirect
	github.com/sergi/go-diff v1.0.0
	github.com/tektoncd/pipeline v0.8.0
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
	google.golang.org/api v0.13.0
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.0.0-20190620084959-7cf5895f2711
	k8s.io/apimachinery v0.0.0-20191030190112-bb31b70367b7
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/utils v0.0.0-20191030222137-2b95a09bc58d // indirect
	knative.dev/pkg v0.0.0-20191031171713-d4ce00139499
)

replace (
	k8s.io/api => k8s.io/api v0.0.0-20191031065753-b19d8caf39be
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20191030190112-bb31b70367b7
)

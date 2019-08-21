#!/usr/bin/env bash

# Copyright 2019 Google, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o pipefail

version=$1
out=release-unversioned.yaml
if [ "$1" != "" ]; then
  out=release-${version}.yaml
fi
out=$(mktemp -d)/${out}

set -o nounset

# Generate release-X.Y.Z.yaml and copy to GCS.
echo Generating ${out}
KO_DOCKER_REPO=gcr.io/gcb-compat ko resolve -t "${version}" -P -f config/  > ${out}
gsutil cp ${out} gs://gcb-compat-releases

# Generate latest Tekton release YAML and copy to GCS.
tmp=$(mktemp -d)
outtmp=$(mktemp -d)
git clone --depth=1 https://github.com/tektoncd/pipeline ${tmp}
KO_DOCKER_REPO=gcr.io/gcb-compat ko resolve -f ${tmp}/config/ > ${outtmp}/tekton.yaml
gsutil cp ${outtmp}/tekton.yaml gs://gcb-compat-releases

#!/bin/bash
#
# Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
set -e

function headers() {
  echo '''/*
Copyright (c) YEAR SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
     http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
'''
}

DIRNAME="$(echo "$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )")"
source "$DIRNAME/common.sh"

header_text "Generate"

rm -f ${GOPATH}/bin/*-gen

CURRENT_DIR=$(dirname $0)
PROJECT_ROOT="${CURRENT_DIR}"/..

# Go modules are not able to properly vendor k8s.io/code-generator@kubernetes-1.14.0 (build with godep)
# to include also generate-groups.sh and generate-internal-groups.sh scripts under vendor/.
# However this is fixed with kubernetes-1.15.0 (k8s.io/code-generator@kubernetes-1.15.0 is opted in go modules).
# The workaround for now is to have a kubernetes-1.14.0 copy of the 2 scripts under hack/code-generator and
# to copy them to vendor/k8s.io/code-generator/ for the generation.
# And respectively clean up them after execution.
# The workaround should be removed adopting kubernetes-1.15.0.
# Similar thing is also done in https://github.com/heptio/contour/pull/1010.

cp "${CURRENT_DIR}"/code-generator/generate-*.sh "${PROJECT_ROOT}"/vendor/k8s.io/code-generator/

cleanup() {
  rm -f "${PROJECT_ROOT}"/vendor/k8s.io/code-generator/generate-groups.sh
}
trap "cleanup" EXIT SIGINT

bash "${PROJECT_ROOT}"/vendor/k8s.io/code-generator/generate-groups.sh \
  "deepcopy" \
  github.com/gardener/gardener-resource-manager/pkg/client/resources \
  github.com/gardener/gardener-resource-manager/pkg/apis \
  resources:v1alpha1 \
  -h <(headers)

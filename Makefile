# Copyright 2023 TV 2 DANMARK A/S
#
# Licensed under the Apache License, Version 2.0 (the "License") with the
# following modification to section 6. Trademarks:
#
# Section 6. Trademarks is deleted and replaced by the following wording:
#
# 6. Trademarks. This License does not grant permission to use the trademarks and
# trade names of TV 2 DANMARK A/S, including but not limited to the TV 2® logo and
# word mark, except (a) as required for reasonable and customary use in describing
# the origin of the Work, e.g. as described in section 4(c) of the License, and
# (b) to reproduce the content of the NOTICE file. Any reference to the Licensor
# must be made by making a reference to "TV 2 DANMARK A/S", written in capitalized
# letters as in this example, unless the format in which the reference is made,
# requires lower case letters.
#
# You may not use this software except in compliance with the License and the
# modifications set out above.
#
# You may obtain a copy of the license at:
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

include Makefile.local

KO_DOCKER_REPO ?= kind.local
KIND_CLUSTER_NAME ?= kind-gwc-dev-cluster
PLATFORMS ?= linux/amd64,linux/arm64
VERSION ?= $(shell git describe --tags --always --dirty --abbrev=12)
CGO_ENABLED = 0
ARCH ?= $(shell uname -o | tr '[:upper:]' '[:lower:]')
CPU ?= $(shell uname -m)

.EXPORT_ALL_VARIABLES:

# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/tv2/bifrost-gateway-controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.32.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests:  ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate:  ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet  ## Run tests.
	KUBEBUILDER_ASSETS="$(shell setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

.PHONY: test-ginkgo
test-ginkgo: manifests generate fmt vet  ## Run tests using ginkgo.
	KUBEBUILDER_ASSETS="$(shell setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" ginkgo -vv ./... -coverprofile cover.out

.PHONY: e2e-test
e2e-test:
	(cd test/e2e/ && USE_EXISTING_CLUSTER=true go test)

.PHONY: unit-test
unit-test:
	kubectl apply -f blueprints/gatewayclassblueprint-contour-istio.yaml -f blueprints/gatewayclass-contour-istio.yaml
	(cd test/unit && USE_EXISTING_CLUSTER=true go test -test.v -gateway-class=contour-istio)

## Runs conformance tests against cluster with controller deployed. Flag `-test.v' can be used to increase logging

.PHONY: conformance-test
conformance-test: ## Only 'core' suite, see https://github.com/kubernetes-sigs/gateway-api/blob/main/conformance/utils/suite/suite.go
	kubectl apply -f blueprints/gatewayclassblueprint-contour-istio.yaml -f blueprints/gatewayclass-contour-istio.yaml
	(cd test/conformance/gateway-api/ && USE_EXISTING_CLUSTER=true go test -gateway-class=contour-istio)

.PHONY: conformance-test-full
conformance-test-full: ## Full suite, see https://github.com/kubernetes-sigs/gateway-api/blob/main/conformance/utils/suite/suite.go
	kubectl apply -f blueprints/gatewayclassblueprint-contour-istio.yaml -f blueprints/gatewayclass-contour-istio.yaml
	(cd test/conformance/gateway-api/ && go test -gateway-class=contour-istio -supported-features=ReferenceGrant,TLSRoute,HTTPRouteQueryParamMatching,HTTPRouteMethodMatching,HTTPResponseHeaderModification,RouteDestinationPortMatching,GatewayClassObservedGenerationBump,HTTPRoutePortRedirect,HTTPRouteSchemeRedirect,HTTPRoutePathRedirect,HTTPRouteHostRewrite,HTTPRoutePathRewrite)

##@ Build

BUILD_COMMIT = $(shell git describe --match="" --always --abbrev=20 --dirty)

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -ldflags="-X main.version=$(VERSION) -X main.commit=$(VERSION) -X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o ./bin/$(ARCH)-$(CPU)/bifrost-gateway-controller .

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

.PHONY: manifest-build
manifest-build: 
	mkdir config/release
	kustomize build config/crd -o config/release/crds.yaml
	kustomize build config/default -o config/release/install.yaml

.PHONY: container
container: ## Build container image with ko.
	@ko build --base-import-paths -t latest -t "sha-$(VERSION)" --platform=$(PLATFORMS) \
		--image-label org.opencontainers.image.source=https://github.com/tv2/bifrost-gateway-controller .

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests  ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	kustomize build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests  ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kustomize build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

CONTROLLER_ARGS ?= --zap-log-level=1

.PHONY: deploy
deploy: manifests  ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && kustomize edit set image controller=${IMG}
	kustomize build config/kind | kubectl apply -f -
	@JSON_ARGS=$$(echo '$(CONTROLLER_ARGS)' | awk '{for(i=1;i<=NF;i++) printf "\"%s\",", $$i}' | sed 's/,$$//'); \
	kubectl patch deployment/bifrost-gateway-controller-controller-manager -n bifrost-gateway-controller-system \
		--type=json -p="[{\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/args\",\"value\":[$$JSON_ARGS]}]"

.PHONY: redeploy
redeploy: container deploy  ## Build, deploy, and restart the controller on the KIND cluster.
	kubectl rollout restart deployment -n bifrost-gateway-controller-system bifrost-gateway-controller-controller-manager
	kubectl rollout status deployment -n bifrost-gateway-controller-system bifrost-gateway-controller-controller-manager --timeout=90s

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kustomize build config/kind | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: lint
lint:
	golangci-lint run -v  --timeout 10m

##@ Helm-docs
.PHONY: helm-docs
helm-docs:
	scripts/helm-docs.sh


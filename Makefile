# Copyright 2021 Ciena Corporation.
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

# Capture version informaton from version file
VERSION=$(shell head -1 ./VERSION)

# Image URL to use all building/pushing image targets
DOCKER_REGISTRY ?= dockerhub.com
DOCKER_TAG ?= $(VERSION)
DOCKER_SCHEDULER_REPOSITORY ?= multi-scheduler
ifeq ($(DOCKER_REGISTRY),)
SCHEDULER_IMG ?= $(DOCKER_SCHEDULER_REPOSITORY):$(DOCKER_TAG)
else
SCHEDULER_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_SCHEDULER_REPOSITORY):$(DOCKER_TAG)
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Setting up the version information for the build
GO_VERSION=$(shell go version 2>/dev/null | cut -d\  -f 3)
GO_ARCH=$(shell go env GOHOSTARCH)
GO_OS=$(shell go env GOHOSTOS)
BUILD_DATE=$(shell date -u "+%Y-%m-%dT%H:%M:%S%Z")
VCS_REF=$(shell git rev-parse HEAD)
ifeq ($(shell git ls-files --others --modified --deleted --exclude-standard | wc -l | tr -d ' '),0)
VCS_DIRTY=false
else
VCS_DIRTY=true
endif
ifeq ($(shell uname -s | tr '[:upper:]' '[:lower:]'),darwin)
VCS_COMMIT_DATE=$(shell date -j -u -f "%Y-%m-%d %H:%M:%S %z" "$(shell git show -s --format=%ci HEAD)" "+%Y-%m-%dT%H:%M:%S%Z")
else
VCS_COMMIT_DATE=$(shell date -u -d "$(shell git show -s --format=%ci HEAD)" "+%Y-%m-%dT%H:%M:%S%Z")
endif
GIT_TRACKING=$(shell git status -b --porcelain=2 | grep branch\.upstream | awk '{print $$3}' | cut -d/ -f1)
ifeq ($(GIT_TRACKING),)
GIT_TRACKING=origin
endif
# Remove any auth information from URL
VCS_URL=$(shell git remote get-url $(GIT_TRACKING) | sed -e 's/\/\/[-_:@a-zA-Z0-9]*[:@]/\/\//g')

DOCKER_BUILD_ARGS=\
--build-arg org_label_schema_version="$(VERSION)" \
--build-arg org_label_schema_vcs_url="$(VCS_URL)" \
--build-arg org_label_schema_vcs_ref="$(VCS_REF)" \
--build-arg org_label_schema_vcs_commit_date="$(VCS_COMMIT_DATE)" \
--build-arg org_label_schema_vcs_dirty="$(VCS_DIRTY)" \
--build-arg org_label_schema_build_date="$(BUILD_DATE)"

LDFLAGS=-ldflags "$(VERSION_LDFLAGS)"

.DEFAULT_GOAL:=help

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

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

##@ Build

.PHONY: build
build: build-scheduler

.PHONY: build-%
build-%: deps-% fmt vet
	@echo "Building $*"
	go build -o bin/$* $(LDFLAGS) ./cmd/$*

deps-scheduler:

.PHONY: docker-build
docker-build: docker-build-scheduler ## Build all docker images.

docker-build-scheduler: ## Build docker image for the constraint policy scheduler.
	docker build $(DOCKER_BUILD_FLAGS) -t $(SCHEDULER_IMG) -f build/Dockerfile.scheduler $(DOCKER_BUILD_ARGS) .

.PHONY: docker-push
docker-push: docker-push-scheduler ## Push all docker images.

.PHONY: docker-push-scheduler
docker-push-scheduler: ## Push the docker image for the constraint policy scheduler.
	docker push $(SCHEDULER_IMG)

.PHONY: clean
clean: ## Delete build and/or temporary artifacts
	rm -rf ./bin *.out

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: deploy deploy-scheduler
deploy: deploy-scheduler

deploy-scheduler:
	sed -e "s;IMAGE_SPEC;$(SCHEDULER_IMG);g" ./deploy/multi-scheduler.yaml | kubectl apply -f -

.PHONY: undeploy undeploy-scheduler
undeploy: undeploy-scheduler 

undeploy-scheduler:
	kubectl delete  --ignore-not-found=$(ignore-not-found) -f ./deploy/multi-scheduler.yaml

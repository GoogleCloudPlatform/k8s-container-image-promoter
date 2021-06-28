# Copyright 2020 The Kubernetes Authors.
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

# If you update this file, please follow
# https://suva.sh/posts/well-documented-makefiles

REPO_ROOT:=$(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

all: test

##@ Build
.PHONY: build
build: ## Go build
	go build -o cip ${REPO_ROOT}/cmd/cip/...
	${REPO_ROOT}/go_with_version.sh build ${REPO_ROOT}/test-e2e/cip-auditor/cip-auditor-e2e.go
	${REPO_ROOT}/go_with_version.sh build ${REPO_ROOT}/test-e2e/cip/e2e.go

.PHONY: install
install: build ## Install
	cp cip $(shell go env GOPATH)/bin

##@ Images

.PHONY: image
image: ## Build image
	bazel build //:cip-docker-loadable.tar

.PHONY: image-load
image-load: image ## Build image and load it
	docker load -i bazel-bin/cip-docker-loadable.tar

.PHONY: image-push
image-push: image ## Build image and push
	bazel run :push-cip

.PHONY: image-push-cip-auditor-e2e
image-push-cip-auditor-e2e: ## Push image cip-auditor-e2e
	./test-e2e/cip-auditor/push-cip-auditor-test.sh

##@ Lints

.PHONY: lint
lint: ## Build image-load-cip-auditor-e2e
	GO111MODULE=on golangci-lint run \
		-v \
		--timeout=5m

.PHONY: lint-ci
lint-ci: download
	make lint

##@ Tests

.PHONY: test
test: test-go-unit ## Runs unit tests

.PHONY: test-go-unit
test-go-unit: ## Runs Golang unit tests
	${REPO_ROOT}/hack/test-go.sh

.PHONY: test-ci
test-ci: download
	make test

.PHONY: test-e2e-cip
test-e2e-cip:
	${REPO_ROOT}/go_with_version.sh run ${REPO_ROOT}/test-e2e/cip/e2e.go \
		-tests=${REPO_ROOT}/test-e2e/cip/tests.yaml \
		-repo-root=${REPO_ROOT} \
		-key-file=${CIP_E2E_KEY_FILE}

.PHONY: test-e2e-cip-auditor
test-e2e-cip-auditor:
	${REPO_ROOT}/go_with_version.sh run ${REPO_ROOT}/test-e2e/cip-auditor/cip-auditor-e2e.go \
		-tests=${REPO_ROOT}/test-e2e/cip-auditor/tests.yaml \
		-repo-root=${REPO_ROOT} \
		-key-file=${CIP_E2E_KEY_FILE}

##@ Dependencies

.PHONY: download update

download: ## Download go modules
	GO111MODULE=on go mod download

update: ## Update go modules (source of truth!).
	GO111MODULE=on go mod verify
	GO111MODULE=on go mod tidy

##@ Verify

.PHONY: verify verify-boilerplate verify-dependencies verify-golangci-lint verify-go-mod verify-archives

verify: verify-boilerplate verify-dependencies verify-golangci-lint verify-go-mod verify-archives ## Runs verification scripts to ensure correct execution

verify-boilerplate: ## Runs the file header check
	./hack/verify-boilerplate.sh

verify-dependencies: ## Runs zeitgeist to verify dependency versions
	./hack/verify-dependencies.sh

verify-go-mod: ## Runs the go module linter
	./hack/verify-go-mod.sh

verify-golangci-lint: ## Runs all golang linters
	./hack/verify-golangci-lint.sh

verify-archives: ### Check golden image archives
	./hack/verify-archives.sh $(REPO_ROOT)

##@ Helpers

.PHONY: help

help:  ## Display this help
	@awk \
		-v "col=${COLOR}" -v "nocol=${NOCOLOR}" \
		' \
			BEGIN { \
				FS = ":.*##" ; \
				printf "\nUsage:\n  make %s<target>%s\n", col, nocol \
			} \
			/^[a-zA-Z_-]+:.*?##/ { \
				printf "  %s%-15s%s %s\n", col, $$1, nocol, $$2 \
			} \
			/^##@/ { \
				printf "\n%s%s%s\n", col, substr($$0, 5), nocol \
			} \
		' $(MAKEFILE_LIST)

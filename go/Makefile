.PHONY: binary, proto

default: binary


# Run go fmt against code
fmt:
	go fmt ./pkg/... ./cmd/...

# Run go vet against code
vet:
	go vet ./pkg/... ./cmd/...

clean:
	-rm -Rf _output

binary: proto
	go build -o scaler cmd/scaler/main.go

docker-build: proto
#	manifest/make-rules/manifest.sh ${IMAGE_REPO}/edge-proxy:${GIT_VERSION} ${DOCKER_USERNAME} ${DOCKER_PASSWD}
	docker buildx build --push ${DOCKER_BUILD_ARGS} --platform linux/amd64 \
    --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 --build-arg GIT_VERSION=${GIT_VERSION} \
    -f Dockerfile . -t registry.cn-hangzhou.aliyuncs.com/longzhou/scaler:1.0

build: proto
#	manifest/make-rules/manifest.sh ${IMAGE_REPO}/edge-proxy:${GIT_VERSION} ${DOCKER_USERNAME} ${DOCKER_PASSWD}
	docker buildx build ${DOCKER_BUILD_ARGS} --platform linux/amd64 \
    --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 --build-arg GIT_VERSION=${GIT_VERSION} \
    -f Dockerfile . -t scaler:1.0

proto:
	cd proto && protoc -I ./  serverless-sim.proto --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative && cd -

delete:
	kubectl delete -f ../manifest/serverless-simulaion.yaml

load:
	kind load docker-image scaler:1.0

create:
	kubectl apply -f ../manifest/serverless-simulaion.yaml

local: build load create

create-cluster:
	kind create cluster --config ../manifest/kind.yaml

delete-cluster:
	kind delete cluster
############# builder #############
FROM golang:1.12.5 AS builder

WORKDIR /go/src/github.com/gardener/gardener-resource-manager
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go install \
  -ldflags "-X github.com/gardener/gardener-resource-manager/pkg/version.gitVersion=$(cat VERSION) \
            -X github.com/gardener/gardener-resource-manager/pkg/version.gitTreeState=$([ -z git status --porcelain 2>/dev/null ] && echo clean || echo dirty) \
            -X github.com/gardener/gardener-resource-manager/pkg/version.gitCommit=$(git rev-parse --verify HEAD) \
            -X github.com/gardener/gardener-resource-manager/pkg/version.buildDate=$(date --rfc-3339=seconds | sed 's/ /T/')" \
  ./...

############# resource-manager #############
FROM alpine:3.9 AS resource-manager

RUN apk add --update bash curl

COPY --from=builder /go/bin/gardener-resource-manager /gardener-resource-manager

WORKDIR /

ENTRYPOINT ["/gardener-resource-manager"]

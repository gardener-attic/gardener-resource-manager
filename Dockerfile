############# builder
FROM eu.gcr.io/gardener-project/3rd/golang:1.15.7 AS builder

WORKDIR /go/src/github.com/gardener/gardener-resource-manager
COPY . .

ARG EFFECTIVE_VERSION
RUN make install EFFECTIVE_VERSION=$EFFECTIVE_VERSION

#############      gardener-resource-manager
FROM eu.gcr.io/gardener-project/3rd/alpine:3.12.3 AS gardener-resource-manager

COPY --from=builder /go/bin/gardener-resource-manager /gardener-resource-manager
ENTRYPOINT ["/gardener-resource-manager"]

############# builder
FROM golang:1.16.5 AS builder

WORKDIR /go/src/github.com/gardener/gardener-resource-manager
COPY . .

ARG EFFECTIVE_VERSION
RUN make install EFFECTIVE_VERSION=$EFFECTIVE_VERSION

#############      gardener-resource-manager
FROM alpine:3.13.5 AS gardener-resource-manager

COPY --from=builder /go/bin/gardener-resource-manager /gardener-resource-manager
ENTRYPOINT ["/gardener-resource-manager"]

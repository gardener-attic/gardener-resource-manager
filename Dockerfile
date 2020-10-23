############# builder
FROM golang:1.15.3 AS builder

WORKDIR /go/src/github.com/gardener/gardener-resource-manager
COPY . .
RUN make install

#############      gardener-resource-manager
FROM alpine:3.12.1 AS gardener-resource-manager

COPY --from=builder /go/bin/gardener-resource-manager /gardener-resource-manager
ENTRYPOINT ["/gardener-resource-manager"]

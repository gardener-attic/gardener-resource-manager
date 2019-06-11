#############      builder-base                             #############
FROM golang:1.12.5 AS builder-base

COPY ./hack/install-requirements.sh /install-requirements.sh
COPY ./tools /tools

RUN /install-requirements.sh

#############      builder                                  #############
FROM builder-base AS builder

ARG VERIFY=true

WORKDIR /go/src/github.com/gardener/gardener-resource-manager
COPY . .

RUN make VERIFY=$VERIFY all

#############      base                                     #############
FROM alpine:3.8 AS base

RUN apk add --update bash curl

WORKDIR /

#############      gardener-resource-manager                #############
FROM base AS gardener-resource-manager

COPY --from=builder /go/bin/gardener-resource-manager /gardener-resource-manager

WORKDIR /

ENTRYPOINT ["/gardener-resource-manager"]

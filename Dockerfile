FROM --platform=$BUILDPLATFORM node:21@sha256:4b232062fa976e3a966c49e9b6279efa56c8d207a67270868f51b3d155c4e33d AS node-build

ARG NPM_REGISTRY=https://registry.npmmirror.com
ENV npm_config_registry=${NPM_REGISTRY}
ENV NPM_CONFIG_REGISTRY=${NPM_REGISTRY}
ENV COREPACK_NPM_REGISTRY=${NPM_REGISTRY}

WORKDIR /app
ADD app/package.json app/pnpm* app/.npmrc .

RUN <<EORUN
#!/bin/bash -e
npm config set registry ${NPM_REGISTRY}
corepack enable
corepack install --global $(node -e 'console.log(require("./package.json").packageManager)')
pnpm config set registry ${NPM_REGISTRY}
pnpm install --silent
EORUN

ADD app/ .
RUN <<EORUN
#!/bin/bash -e
pnpm run build
mkdir /artifacts
mv appearance stage guide changelogs /artifacts/
EORUN

FROM golang:1.25-alpine@sha256:8d95af53d0d58e1759ddb4028285d9b1239067e4fbf4f544618cad0f60fbc354 AS go-build

RUN <<EORUN
#!/bin/sh -e
apk add --no-cache gcc musl-dev
go env -w GO111MODULE=on
go env -w CGO_ENABLED=1
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.google.cn
EORUN

WORKDIR /kernel
ADD kernel/go.* .
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    go mod download

ADD kernel/ .
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    go build -tags fts5 -v -ldflags "-s -w"
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg \
    go build -o /kernel/siyuan-multi -v -ldflags "-s -w" ./cmd/siyuan-multi

FROM alpine:latest@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
LABEL maintainer="Liang Ding<845765@qq.com>"

RUN apk add --no-cache ca-certificates tzdata su-exec

ENV TZ=Asia/Shanghai
ENV HOME=/home/siyuan
ENV RUN_IN_CONTAINER=true
EXPOSE 30010 30009 30008 30007 30006

WORKDIR /opt/siyuan/
COPY --from=go-build --chmod=755 /kernel/kernel /kernel/siyuan-multi /kernel/entrypoint.sh .
COPY --from=node-build /artifacts .

ENTRYPOINT ["/opt/siyuan/entrypoint.sh"]
CMD ["/opt/siyuan/kernel"]

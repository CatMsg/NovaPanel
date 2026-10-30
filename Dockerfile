FROM --platform=$BUILDPLATFORM node:alpine AS front-builder
ARG TARGETARCH
RUN test "$TARGETARCH" = "amd64" || { echo "Linux Docker images are supported on amd64 only" >&2; exit 1; }
WORKDIR /app
COPY frontend/ ./
RUN npm ci && npm run build

FROM golang:1.26.10-alpine AS backend-builder
WORKDIR /app
ARG TARGETARCH
RUN test "$TARGETARCH" = "amd64" || { echo "Linux Docker images are supported on amd64 only" >&2; exit 1; }
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
ENV GOARCH=$TARGETARCH

RUN apk update && apk add --no-cache \
    gcc \
    musl-dev \
    libc-dev \
    make \
    git \
    wget \
    unzip \
    bash \
    curl

ENV CC=gcc

COPY third_party/sing-box/.github/CRONET_GO_VERSION /tmp/CRONET_GO_VERSION
RUN set -eux; \
    CRONET_GO_VERSION="$(tr -d '\r\n' < /tmp/CRONET_GO_VERSION)"; \
    printf '%s\n' "$CRONET_GO_VERSION" | grep -Eq '^[0-9a-f]{40}$'; \
    git init /tmp/cronet-go-lib; \
    git -C /tmp/cronet-go-lib remote add origin https://github.com/sagernet/cronet-go.git; \
    git -C /tmp/cronet-go-lib sparse-checkout set --no-cone /lib/linux_amd64/libcronet.so; \
    git -C /tmp/cronet-go-lib fetch --depth=1 --filter=blob:none origin "$CRONET_GO_VERSION"; \
    git -C /tmp/cronet-go-lib checkout FETCH_HEAD; \
    install -m 755 /tmp/cronet-go-lib/lib/linux_amd64/libcronet.so ./libcronet.so; \
    rm -rf /tmp/cronet-go-lib /tmp/CRONET_GO_VERSION

COPY . .
COPY --from=front-builder /app/dist/ /app/web/html/

RUN go build -ldflags="-w -s" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" \
    -o novas .

RUN CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" bash ./scripts/build-mita.sh /app/mita

FROM alpine
ENV TZ=Asia/Shanghai
WORKDIR /app
ARG TARGETARCH
RUN set -ex && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=backend-builder /app/novas /app/libcronet.so /app/
COPY --from=backend-builder /app/mita /app/bin/mita
COPY LICENSE THIRD_PARTY_NOTICES.md /app/
COPY third_party/sing-box/LICENSE /app/licenses/sing-box-LICENSE
COPY entrypoint.sh /app/
COPY scripts/hy2-forward.sh /app/scripts/hy2-forward.sh
ENTRYPOINT [ "./entrypoint.sh" ]

FROM --platform=$BUILDPLATFORM node:alpine AS front-builder
WORKDIR /app
COPY frontend/ ./
RUN npm install && npm run build

FROM golang:1.26.5-alpine AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
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

RUN CRONET_ARCH="$TARGETARCH" && \
    CRONET_URL="https://github.com/SagerNet/cronet-go/releases/latest/download/libcronet-linux-${CRONET_ARCH}.so"; \
    echo "Downloading $CRONET_URL" && \
    wget -q -O ./libcronet.so "$CRONET_URL" && \
    chmod 755 ./libcronet.so

COPY . .
COPY --from=front-builder /app/dist/ /app/web/html/

RUN if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" \
    -o sui main.go

FROM alpine
ENV TZ=Asia/Shanghai
WORKDIR /app
ARG TARGETARCH
RUN set -ex && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=backend-builder /app/sui /app/libcronet.so /app/
RUN set -ex; \
    test "$TARGETARCH" = "amd64"; \
    mkdir -p /app/bin; \
    wget -O /tmp/mita.tar.gz \
      https://github.com/enfein/mieru/releases/download/v3.34.1/mita_3.34.1_linux_amd64.tar.gz; \
    echo "499c7390406175a32c140bf31b8b3e1fc2abfe7f4d523e067f09a6fc461e6325  /tmp/mita.tar.gz" | sha256sum -c -; \
    tar -xzf /tmp/mita.tar.gz -C /app/bin mita; \
    chmod 755 /app/bin/mita; \
    rm -f /tmp/mita.tar.gz
COPY LICENSE THIRD_PARTY_NOTICES.md /app/
COPY entrypoint.sh /app/
ENTRYPOINT [ "./entrypoint.sh" ]

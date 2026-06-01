FROM golang:1.23-alpine@sha256:383395b794dffa5b53012a212365d40c8e37109a626ca30d6151c8348d380b5f AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hath-natmap ./cmd/hath-natmap

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS binary-downloader

ARG TARGETARCH
ARG TARGETVARIANT
ARG NATMAP_VERSION=20260214
ARG HATH_RUST_VERSION=v1.17.0

RUN apk add --no-cache ca-certificates wget
COPY docker/checksums.txt /checksums.txt
RUN set -eux; \
    case "${TARGETARCH}/${TARGETVARIANT}" in \
      "amd64/") \
        NATMAP_ASSET="natmap-linux-x86_64"; \
        HATH_ASSET="hath-rust-x86_64-unknown-linux-musl"; \
        ;; \
      "arm64/"|"arm64/v8") \
        NATMAP_ASSET="natmap-linux-arm64"; \
        HATH_ASSET="hath-rust-aarch64-unknown-linux-musl"; \
        ;; \
      "arm/v7") \
        NATMAP_ASSET="natmap-linux-arm32v7hf"; \
        HATH_ASSET="hath-rust-armv7-unknown-linux-musleabihf"; \
        ;; \
      *) \
        echo "Unsupported platform: ${TARGETARCH}/${TARGETVARIANT}" >&2; \
        exit 1; \
        ;; \
    esac; \
    wget -O "/tmp/${NATMAP_ASSET}" "https://github.com/heiher/natmap/releases/download/${NATMAP_VERSION}/${NATMAP_ASSET}"; \
    wget -O "/tmp/${HATH_ASSET}" "https://github.com/james58899/hath-rust/releases/download/${HATH_RUST_VERSION}/${HATH_ASSET}"; \
    test "$(grep -c "  ${NATMAP_ASSET}$" /checksums.txt)" -eq 1; \
    test "$(grep -c "  ${HATH_ASSET}$" /checksums.txt)" -eq 1; \
    cd /tmp; \
    grep "  ${NATMAP_ASSET}$" /checksums.txt | sha256sum -c -; \
    grep "  ${HATH_ASSET}$" /checksums.txt | sha256sum -c -; \
    mv "/tmp/${NATMAP_ASSET}" /out-natmap; \
    mv "/tmp/${HATH_ASSET}" /out-hath-rust; \
    chmod 0755 /out-natmap /out-hath-rust

FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc

RUN apk add --no-cache ca-certificates iproute2 shadow su-exec tzdata \
    && apk add --no-cache --virtual .cap-tools libcap \
    && addgroup -S hath \
    && adduser -S -D -H -G hath -h /app -s /sbin/nologin hath \
    && mkdir -p /app /config /data/hath /run/hath-natmap
WORKDIR /app

COPY --from=go-builder /out/hath-natmap /usr/local/bin/hath-natmap
COPY --from=binary-downloader /out-natmap /usr/local/bin/natmap
COPY --from=binary-downloader /out-hath-rust /usr/local/bin/hath-rust
COPY scripts/natmap-notify.sh /usr/local/bin/natmap-notify.sh
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
COPY configs/config.example.yaml /config/config.example.yaml

RUN chmod 0755 /usr/local/bin/hath-natmap /usr/local/bin/natmap /usr/local/bin/hath-rust /usr/local/bin/natmap-notify.sh /usr/local/bin/entrypoint.sh \
    && setcap cap_net_admin+ep "$(command -v tc)" \
    && setcap -v cap_net_admin+ep "$(command -v tc)" \
    && cp /config/config.example.yaml /config/config.yaml \
    && chown -R hath:hath /app /config /data/hath /run/hath-natmap \
    && chmod 0700 /data/hath /run/hath-natmap \
    && chmod 0755 /config \
    && chmod 0644 /config/config.example.yaml /config/config.yaml \
    && apk del .cap-tools

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["/usr/local/bin/hath-natmap", "--config", "/config/config.yaml"]

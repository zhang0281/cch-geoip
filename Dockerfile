# syntax=docker/dockerfile:1
ARG GO_IMAGE=golang:1.26.9-alpine
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/cch-geoip ./cmd/cch-geoip && mkdir -p /out/cache /out/ip2region
ADD --checksum=sha256:8e31bbdccb5bf21028af10592d4312ec975da0bffa108c0c5d862a12190f9ad3 https://raw.githubusercontent.com/lionsoul2014/ip2region/v3.18.0/data/ip2region_v4.xdb /out/ip2region/ip2region_v4.xdb
ADD --checksum=sha256:939f6b46bd2b8bec3cf7c5ceb8ba782266ae9b1f35b5ba7916700dec0b7506ed https://raw.githubusercontent.com/lionsoul2014/ip2region/v3.18.0/data/ip2region_v6.xdb /out/ip2region/ip2region_v6.xdb
RUN chmod 644 /out/ip2region/*.xdb

FROM scratch
COPY --from=build /out/cch-geoip /cch-geoip
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chown=65532:65532 --from=build /out/cache/ /var/lib/cch-geoip/
COPY --chown=65532:65532 --from=build /out/ip2region/ /usr/share/ip2region/v3.18.0/
COPY --chown=65532:65532 third_party/ip2region/LICENSE.md /usr/share/ip2region/LICENSE.md
USER 65532:65532
ENV GEOIP_LISTEN=0.0.0.0:8080
ENV IP2REGION_ENABLED=true IP2REGION_VERSION=v3.18.0 IP2REGION_DATA_DIR=/usr/share/ip2region
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD ["/cch-geoip", "healthcheck"]
ENTRYPOINT ["/cch-geoip"]

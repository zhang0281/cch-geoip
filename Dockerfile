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
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/cch-geoip ./cmd/cch-geoip && mkdir -p /out/cache

FROM scratch
COPY --from=build /out/cch-geoip /cch-geoip
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chown=65532:65532 --from=build /out/cache/ /var/lib/cch-geoip/
USER 65532:65532
ENV GEOIP_LISTEN=0.0.0.0:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD ["/cch-geoip", "healthcheck"]
ENTRYPOINT ["/cch-geoip"]

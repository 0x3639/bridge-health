FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM scratch AS reporter
COPY --from=build /out/bridge-health-reporter /bridge-health-reporter
USER 65532:65532
ENTRYPOINT ["/bridge-health-reporter"]

FROM build AS receiver-files
RUN mkdir -p /data/receiver /downloads && chown -R 65532:65532 /data && chmod 0700 /data/receiver
RUN for arch in amd64 arm64; do \
      CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags="-s -w" \
        -o /downloads/bridge-health-reporter-linux-$arch ./cmd/bridge-health-reporter || exit 1; \
    done

FROM scratch AS receiver
COPY --from=receiver-files /out/bridge-health-receiver /bridge-health-receiver
COPY --from=receiver-files /downloads /downloads
COPY --from=receiver-files --chown=65532:65532 /data /data
USER 65532:65532
EXPOSE 8080 8443
ENTRYPOINT ["/bridge-health-receiver"]

FROM scratch AS collector
COPY --from=build /out/bridge-health-collector /bridge-health-collector
USER 65532:65532
ENTRYPOINT ["/bridge-health-collector"]

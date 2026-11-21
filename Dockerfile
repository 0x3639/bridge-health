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

FROM scratch AS collector
COPY --from=build /out/bridge-health-collector /bridge-health-collector
USER 65532:65532
ENTRYPOINT ["/bridge-health-collector"]

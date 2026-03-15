# Node Probe DaemonSet
FROM golang:1.25-alpine AS builder
ARG IMAGE_TAG=latest
RUN apk add --no-cache git ca-certificates
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -ldflags="-s -w" -o /node-probe ./cmd/node-probe

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /node-probe /node-probe
USER 65532:65532
ENTRYPOINT ["/node-probe"]

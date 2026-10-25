FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir /data && chown 65532:65532 /data
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o assistant-service ./cmd/main.go

FROM gcr.io/distroless/static-debian12
WORKDIR /
COPY --from=builder /app/assistant-service /assistant-service
COPY --from=builder --chown=65532:65532 /data /data
USER nonroot:nonroot
EXPOSE 8093
ENTRYPOINT ["/assistant-service"]

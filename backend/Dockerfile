FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/cmd ./cmd
ARG BUILD_CPU_COUNT=1
RUN CGO_ENABLED=0 GOMAXPROCS="${BUILD_CPU_COUNT}" go build -p "${BUILD_CPU_COUNT}" -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/server /app/server
ENV TZ=Asia/Shanghai
ENV BACKEND_ADDR=:15681
USER 65532:65532
EXPOSE 15681
ENTRYPOINT ["/app/server"]

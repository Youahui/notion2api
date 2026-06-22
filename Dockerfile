FROM golang:1.25-alpine AS backend-build

WORKDIR /src/backend
RUN apk add --no-cache build-base
COPY backend/go.mod ./
RUN go mod download
COPY backend/ ./
RUN go build -o /out/notion2api ./cmd/server

FROM alpine:3.20

WORKDIR /app
RUN adduser -D -h /app notion2api
COPY --from=backend-build /out/notion2api ./notion2api
COPY backend/configs ./config
RUN mkdir -p /app/data && chown -R notion2api:notion2api /app
USER notion2api
EXPOSE 9998
CMD ["./notion2api", "--config", "/app/config/config.docker.yaml"]

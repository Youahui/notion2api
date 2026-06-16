FROM golang:1.22-alpine AS backend-build

WORKDIR /src/backend
RUN apk add --no-cache build-base
COPY backend/go.mod ./
RUN go mod download
COPY backend/ ./
RUN go build -o /out/notaion2api ./cmd/server

FROM alpine:3.20

WORKDIR /app
RUN adduser -D -h /app notaion2api
COPY --from=backend-build /out/notaion2api ./notaion2api
COPY backend/configs ./config
RUN mkdir -p /app/data && chown -R notaion2api:notaion2api /app
USER notaion2api
EXPOSE 8787
CMD ["./notaion2api", "--config", "/app/config/config.docker.yaml"]

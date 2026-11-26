# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN mkdir -p ../internal/server/dist && npm run build

FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/server/dist ./internal/server/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/datafluxor-seo ./cmd/datafluxor-seo \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/datafluxor-seo /usr/local/bin/datafluxor-seo
COPY --from=build --chown=65532:65532 /out/data /data
ENV DFSEO_ADDR=:8080 DFSEO_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/usr/local/bin/datafluxor-seo", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/datafluxor-seo"]

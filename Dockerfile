# 多阶段构建分别产出 React 静态站点与最小 Go API/Web 运行时镜像。
ARG NODE_BUILD_IMAGE=node:24-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d
ARG GO_BUILD_IMAGE=golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651
ARG RUNTIME_IMAGE=alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
ARG GO_MODULE_PROXY=https://proxy.golang.org,direct

FROM ${GO_BUILD_IMAGE} AS go-build
ARG GO_MODULE_PROXY
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN GOPROXY="${GO_MODULE_PROXY}" go mod download
COPY server ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/travel-api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/travel-web ./cmd/web

FROM ${RUNTIME_IMAGE} AS api
RUN adduser -S -D -H -u 10001 travel
COPY --from=go-build /out/travel-api /usr/local/bin/travel-api
USER travel
EXPOSE 8502
ENTRYPOINT ["travel-api"]

FROM ${NODE_BUILD_IMAGE} AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
ARG APP_BASE_PATH=travel
# 云端小内存实例没有 swap；限制 Node 堆，避免构建挤占运行中生产容器的内存。
RUN NODE_OPTIONS=--max-old-space-size=512 VITE_BASE_PATH=/${APP_BASE_PATH}/ npm run build

FROM ${RUNTIME_IMAGE} AS web
RUN adduser -S -D -H -u 10001 travel
COPY --from=go-build /out/travel-web /usr/local/bin/travel-web
COPY --from=web-build /src/web/dist /srv/web
USER travel
EXPOSE 8501
ENTRYPOINT ["travel-web"]

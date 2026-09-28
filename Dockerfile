# Skill Platform - 多阶段 Dockerfile
# 阶段 1: 构 SvelteKit 前端
# 阶段 2: Go 编译后端，嵌入前端产物
# 阶段 3: 最终运行时镜像（alpine，~50MB）

FROM node:23-alpine AS sveltekit
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm install --global npm@11.19.0 && npm ci
COPY frontend ./
RUN npm run build

FROM golang:1.24-alpine AS pocketbase
WORKDIR /app
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
# 嵌入 SvelteKit 构建产物到 pb_public/
COPY --from=sveltekit /app/build /app/pb_public
RUN go build -ldflags="-s -w" -o pocketbase main.go

FROM alpine:3.21
WORKDIR /app
RUN apk --update add ca-certificates
COPY --from=pocketbase /app/pocketbase /app/pocketbase
EXPOSE 8090
# 启动时从 config.env 读 MINIMAX_API_KEY
ENTRYPOINT ["/app/pocketbase", "serve", "--http=0.0.0.0:8090"]

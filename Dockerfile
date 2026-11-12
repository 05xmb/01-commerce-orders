# 交易系统一体化镜像：trade-api + pay-channel 双二进制 + SPA 静态资源
# 运行角色由环境变量 ROLE 选择：server（默认）/ channel / web（nginx 托管 SPA）

FROM golang:1.27.3-alpine AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/trade-api ./cmd/server \
 && CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/pay-channel ./cmd/channel

FROM node:20-alpine AS webbuild
WORKDIR /web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web .
RUN pnpm build

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata nginx
WORKDIR /app
COPY --from=gobuild /out/trade-api /out/pay-channel /app/
COPY --from=webbuild /web/dist /app/web/dist
COPY etc/server.yaml etc/channel.yaml /app/etc/
COPY deploy/nginx.conf /app/etc/nginx.conf
COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
ENV ROLE=server
EXPOSE 8888 8901 80
ENTRYPOINT ["/app/entrypoint.sh"]

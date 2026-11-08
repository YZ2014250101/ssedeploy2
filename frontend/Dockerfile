# ---------- 构建阶段：打包静态资源 ----------
FROM node:20-alpine AS builder

WORKDIR /app

# 先装依赖，利用镜像层缓存
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

# 再拷贝源码并构建
COPY . .
RUN npm run build

# ---------- 运行阶段：Nginx 托管静态文件并反向代理后端 ----------
FROM nginx:1.27-alpine

# 自定义 Nginx 配置：SPA history 路由 + /api 反向代理（含 WebSocket 升级）
COPY nginx.conf /etc/nginx/conf.d/default.conf

# 拷贝构建产物
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

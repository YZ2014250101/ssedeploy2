#!/bin/sh
# =============================================================================
# 后端容器入口脚本
#
# 作用：
#   1. 等待内嵌 K3s 生成 kubeconfig（k3s 容器启动需要数十秒）；
#   2. 把 kubeconfig 中的 127.0.0.1 / localhost 改写为容器网络可达的 k3s 服务名；
#   3. 设置 SSD_KUBECONFIG 后启动后端二进制。
#
# 说明：k3s 与 backend 运行在不同容器，127.0.0.1:6443 指向 backend 自身，
#       所以必须改写；k3s 启动时已通过 --tls-san=k3s 把服务名加入证书 SAN。
# =============================================================================
set -eu

KUBE_SOURCE="${KUBE_SOURCE:-/kube/k3s.yaml}"
KUBE_DEST="${KUBE_DEST:-/tmp/kubeconfig}"
K3S_API_SERVER="${K3S_API_SERVER:-https://k3s:6443}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-180}"

echo "[entrypoint] 等待 K3s kubeconfig: ${KUBE_SOURCE}"
waited=0
while [ ! -s "${KUBE_SOURCE}" ]; do
  waited=$((waited + 2))
  if [ "${waited}" -ge "${MAX_WAIT_SECONDS}" ]; then
    echo "[entrypoint] 等待超时（${MAX_WAIT_SECONDS}s），请检查 k3s 容器日志：docker logs ssedeploy-k3s" >&2
    exit 1
  fi
  sleep 2
done

# 改写 server 地址（同时兼容 127.0.0.1 与 localhost 两种写法）
sed -e "s#https://127.0.0.1:6443#${K3S_API_SERVER}#g" \
    -e "s#https://localhost:6443#${K3S_API_SERVER}#g" \
    "${KUBE_SOURCE}" > "${KUBE_DEST}"
chmod 600 "${KUBE_DEST}"

export SSD_KUBECONFIG="${KUBE_DEST}"
echo "[entrypoint] K3s API: ${K3S_API_SERVER}，启动后端..."

# exec 保证后端进程成为 PID 1，docker stop 能正常传递信号
exec /app/ssedeploy

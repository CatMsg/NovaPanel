#!/usr/bin/env bash
# 测试受支持的 Linux Docker 构建目标（amd64）

set -euo pipefail
cd "$(dirname "$0")"

PLATFORM="linux/amd64"
echo "==> 正在测试 Docker 构建平台：$PLATFORM"
docker buildx build \
  --platform "$PLATFORM" \
  -f Dockerfile \
  --progress=plain \
  . 2>&1 | tee docker-build-test.log

echo "==> 完成。完整输出请查看 docker-build-test.log。"

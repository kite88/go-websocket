#!/usr/bin/env bash
#
# go-websocket 启动脚本：先切到脚本所在目录，再拉起同目录下的可执行文件，
# 因此可以在任意位置调用，不需要先 cd 进解压目录。
#
#   ./start.sh                      # 内嵌配置，外加同目录的 env.ini（如果存在）
#   ./start.sh -config env.ini      # 附加参数会原样透传给 go-websocket
#
# 页面、静态资源与默认配置都内嵌在可执行文件里，没有 env.ini 也能直接跑；
# 配置优先级：-config 指定 > ./env.ini > ./config/env.ini > 内嵌模板。

set -euo pipefail
cd "$(dirname "$0")"
exec ./go-websocket "$@"

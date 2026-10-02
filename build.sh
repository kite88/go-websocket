#!/usr/bin/env bash
#
# 交叉编译 go-websocket 并生成发布产物到 dist/。
#
# 页面、静态资源与配置模板都已经 //go:embed 进二进制，所以每个平台只需要一个可执行
# 文件：下载后直接运行即可，不需要附带任何外部文件。
#
# 产物：dist/go-websocket_<版本>_<os>_<arch>[.exe]，外加 checksums.txt
# （LF 换行，Linux / macOS 下可直接 sha256sum -c checksums.txt 校验）。
#
# 版本号按 -v 参数 > VERSION 环境变量 > git describe --tags 的顺序取值，并注入
# main.version，`go-websocket -version` 会打印它。
#
# 用法: ./build.sh [-v 版本号] [-o 输出目录]

set -euo pipefail

usage() {
    cat <<'EOF'
交叉编译 go-websocket 并生成发布产物。

用法: ./build.sh [-v 版本号] [-o 输出目录]

选项:
  -v 版本号    注入二进制的版本号（默认: $VERSION > git describe --tags > dev）
  -o OutDir    输出目录（默认: dist，相对本脚本所在目录）
  -h           显示本帮助

平台矩阵: windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
每个平台一个可执行文件，外加 checksums.txt。
EOF
}

version=''
out_dir=dist
while getopts ':hv:o:' opt; do
    case "$opt" in
        h) usage; exit 0 ;;
        v) version=$OPTARG ;;
        o) out_dir=$OPTARG ;;
        '?') usage >&2; exit 2 ;;
    esac
done

cd "$(dirname "$0")"

if [ -z "$version" ]; then
    version=${VERSION:-}
fi
if [ -z "$version" ]; then
    version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
fi

if [ -z "$out_dir" ]; then
    echo "OutDir 不能为空" >&2
    exit 2
fi
case "$out_dir" in
    /*) ;;
    *) out_dir=$PWD/$out_dir ;;
esac

# 目标矩阵，'os/arch' 形式；增删一行即可调整发布平台。
targets='windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64'

# 依赖全是纯 Go（gin / gorilla-websocket / ini），交叉编译不需要 cgo。
export CGO_ENABLED=0

mkdir -p "$out_dir"
# 只删除输出目录顶层的文件，不递归，避免误伤目录外的东西
find "$out_dir" -maxdepth 1 -type f -delete

count=0
for target in $targets; do
    goos=${target%%/*}
    goarch=${target#*/}

    bin_name="go-websocket_${version}_${goos}_${goarch}"
    if [ "$goos" = windows ]; then
        bin_name="$bin_name.exe"
    fi

    GOOS="$goos" GOARCH="$goarch" go build -trimpath \
        -ldflags "-s -w -X main.version=$version" \
        -o "$out_dir/$bin_name" .

    size=$(du -h "$out_dir/$bin_name" | cut -f1)
    printf '  build %-16s -> %s  (%s)\n' "$target" "$bin_name" "$size"
    count=$((count + 1))
done

# 产物的 SHA256；只统计可执行文件，LF 换行以便 sha256sum -c
hash_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1   # macOS
    fi
}
find "$out_dir" -maxdepth 1 -type f -name 'go-websocket_*' | LC_ALL=C sort | while IFS= read -r file; do
    printf '%s  %s\n' "$(hash_of "$file")" "$(basename "$file")"
done > "$out_dir/checksums.txt"

echo
echo "版本 $version：$count 个可执行文件 + checksums.txt -> $out_dir"

#!/usr/bin/env bash
#
# 交叉编译 go-websocket 并按平台打包成可分发的归档，产物落在 dist/。
#
# 每个归档是一个顶层目录 go-websocket-<os>-<arch>/，解压后直接可用：
#
#   go-websocket-<os>-<arch>/
#   ├── go-websocket(.exe)   可执行文件（非 Windows 平台带 0755 权限）
#   ├── start.sh             启动脚本（Windows 为 start.bat，带 0755 权限）
#   ├── env.ini              默认配置（页面与静态资源已内嵌，没有它也能跑）
#   └── LICENSE
#
# Windows 用 .zip，其余平台用 .tar.gz；另有 checksums.txt 记录全部归档的 SHA256
# （LF 换行，Linux / macOS 下可直接 sha256sum -c checksums.txt）。
#
# 版本号按 -v 参数 > VERSION 环境变量 > git describe --tags 的顺序取值，并注入
# main.version，`go-websocket -version` 会打印它。
#
# 用法: ./build.sh [-v 版本号] [-o 输出目录]

set -euo pipefail

usage() {
    cat <<'EOF'
交叉编译 go-websocket 并打包为可分发的归档。

用法: ./build.sh [-v 版本号] [-o 输出目录]

选项:
  -v 版本号    注入二进制的版本号（默认: $VERSION > git describe --tags > dev）
  -o OutDir    输出目录（默认: dist，相对本脚本所在目录）
  -h           显示本帮助

平台矩阵: windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
每个平台一个归档（Windows 为 .zip，其余为 .tar.gz），外加 checksums.txt。
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

staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT

# zip 不是 Git for Windows 自带的命令，缺失时退回 PowerShell 的 Compress-Archive，
# 这样 Windows 本地也能跑完整流程（CI 上是 Linux，直接有 zip）。
make_zip() {
    pkg_name=$1
    archive_name=$2
    if command -v zip >/dev/null 2>&1; then
        (cd "$staging" && zip -qr "$out_dir/$archive_name" "$pkg_name")
    else
        powershell.exe -NoProfile -Command \
            "Compress-Archive -Path '$(cygpath -w "$staging/$pkg_name")' -DestinationPath '$(cygpath -w "$out_dir/$archive_name")' -Force" >/dev/null
    fi
}

count=0
for target in $targets; do
    goos=${target%%/*}
    goarch=${target#*/}

    # 归档内保留短名与顶层目录名，都不带版本号，版本靠 Release 标签区分
    bin_name=go-websocket
    if [ "$goos" = windows ]; then
        bin_name=go-websocket.exe
    fi
    pkg_name="go-websocket-$goos-$goarch"
    if [ "$goos" = windows ]; then
        archive_name="$pkg_name.zip"
    else
        archive_name="$pkg_name.tar.gz"
    fi

    pkg_dir="$staging/$pkg_name"
    rm -rf "$pkg_dir"
    mkdir -p "$pkg_dir"

    GOOS="$goos" GOARCH="$goarch" go build -trimpath \
        -ldflags "-s -w -X main.version=$version" \
        -o "$pkg_dir/$bin_name" .
    cp config/env.ini.release "$pkg_dir/env.ini"
    cp LICENSE "$pkg_dir/LICENSE"

    # 启动脚本自身会切到所在目录，因此可在任意位置调用
    if [ "$goos" = windows ]; then
        cp tools/release/start.bat "$pkg_dir/start.bat"
    else
        cp tools/release/start.sh "$pkg_dir/start.sh"
    fi

    if [ "$goos" = windows ]; then
        # zip 不保存可执行位，Windows 也不需要
        make_zip "$pkg_name" "$archive_name"
    else
        chmod 0755 "$pkg_dir/$bin_name" "$pkg_dir/start.sh"
        # 固定 mtime，保证同一份源码重复构建产物一致
        find "$pkg_dir" -exec touch -t 202601010000.00 {} +
        (cd "$staging" && tar -czf "$out_dir/$archive_name" "$pkg_name")
    fi

    size=$(du -h "$out_dir/$archive_name" | cut -f1)
    printf '  build %-16s -> %s  (%s)\n' "$target" "$archive_name" "$size"
    count=$((count + 1))
done

# 归档的 SHA256；只统计要发布的归档，LF 换行以便 sha256sum -c
hash_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1   # macOS
    fi
}
find "$out_dir" -maxdepth 1 -type f \( -name '*.zip' -o -name '*.tar.gz' \) | LC_ALL=C sort | while IFS= read -r file; do
    printf '%s  %s\n' "$(hash_of "$file")" "$(basename "$file")"
done > "$out_dir/checksums.txt"

echo
echo "版本 $version：$count 个归档 + checksums.txt -> $out_dir"

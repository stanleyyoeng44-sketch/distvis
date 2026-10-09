// Explicit registry-1.docker.io references avoid Docker Hub's configured mirror
// lookup for unqualified names such as golang:1.24-alpine.
export function buildImages(env = process.env) {
  const images = {
    go: env.DISTVIS_GO_IMAGE || 'registry-1.docker.io/library/golang:1.24-alpine',
    runtime: env.DISTVIS_RUNTIME_IMAGE || 'registry-1.docker.io/library/alpine:3.21',
  };
  for (const [key, value] of Object.entries(images)) {
    // The refs are written to FROM instructions. Reject whitespace, shell expansion
    // and instruction injection, but allow ports, tags and digest-pinned references.
    if (!/^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$/.test(value) || value.length > 512) {
      throw new Error(`${key === 'go' ? 'DISTVIS_GO_IMAGE' : 'DISTVIS_RUNTIME_IMAGE'} 不是合法的镜像引用`);
    }
  }
  return images;
}

export function dockerfile(images) {
  return [
    `FROM ${images.go} AS build`,
    'WORKDIR /src',
    'ARG GOPROXY=https://proxy.golang.org,direct',
    'COPY . .',
    'RUN set -eu; mkdir /out; for n in cmd/*; do CGO_ENABLED=0 go build -o /out/$(basename "$n") ./$n; done',
    `FROM ${images.runtime}`,
    'RUN adduser -D -u 10001 student && mkdir /state && chown student /state',
    'COPY --from=build /out /app',
    'USER student',
    'WORKDIR /state',
    'CMD ["sleep", "infinity"]',
  ].join('\n');
}

// Directory projects compile inside an init container from the mounted snapshot.
// Cache the SDK and its dependencies in a reusable image layer, not user code.
export function projectDockerfile(images) {
  return [
    `FROM ${images.go}`,
    'ARG GOPROXY=https://proxy.golang.org,direct',
    'ENV GOCACHE=/opt/go-cache GOWORK=off CGO_ENABLED=0',
    'WORKDIR /opt/distvis',
    'COPY go.mod go.sum ./',
    'RUN go mod download',
    'COPY sdk/ ./sdk/',
    'RUN go build ./sdk/...',
    'RUN adduser -D -u 10001 student && mkdir /state && chown student /state',
    'USER student',
    'WORKDIR /state',
    'CMD ["sleep", "infinity"]',
  ].join('\n');
}
export const compileProject = [
  'set -eu',
  'mkdir -p /tmp/distvis-build /app',
  'cp -R /protocol/. /tmp/distvis-build/',
  'cd /tmp/distvis-build',
  'go mod edit -replace=distvis=/opt/distvis',
  'go build -mod=mod -o /app/node "$DISTVIS_ENTRY"',
  'chmod 755 /app/node',
].join('\n');

export function describeBuildFailure(error) {
  const text = error.message;
  let hint;
  if (text.includes('yourcode.mirror.aliyuncs.com')) {
    hint = 'Docker 镜像加速器使用了占位地址 yourcode.mirror.aliyuncs.com。请在 Docker Desktop → Settings → Docker Engine 的 registry-mirrors 中移除此地址，或填写可用的镜像加速器，然后 Apply & restart。';
  } else if (/failed to resolve source metadata|failed to authorize|pull access denied|no such host|dial tcp|403 Forbidden|429 Too Many Requests/i.test(text)) {
    hint = '基础镜像下载失败，尚未进入 Go 编译。请检查仓库连通性与认证；可用 DISTVIS_GO_IMAGE、DISTVIS_RUNTIME_IMAGE 指定可访问的基础镜像。';
  } else if (/permission denied.*docker|Cannot connect to the Docker daemon|docker daemon is not running|ENOENT/i.test(text)) {
    hint = '无法访问 Docker。请确认 Docker Desktop 已启动、docker 命令可用且当前进程有权限访问 Docker daemon。';
  } else {
    hint = '镜像构建失败。请展开事件详情查看下方完整输出中的 Go 编译错误或 Docker 错误。';
  }
  return `${hint}\n\n${text}`;
}

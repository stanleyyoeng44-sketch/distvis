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
      throw new Error(`${key === 'go' ? 'DISTVIS_GO_IMAGE' : 'DISTVIS_RUNTIME_IMAGE'} is not a valid image reference`);
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
    hint = 'The Docker registry mirror uses the placeholder address yourcode.mirror.aliyuncs.com. Remove it from registry-mirrors in Docker Desktop → Settings → Docker Engine, or set a working mirror, then Apply & restart.';
  } else if (/failed to resolve source metadata|failed to authorize|pull access denied|no such host|dial tcp|403 Forbidden|429 Too Many Requests/i.test(text)) {
    hint = 'Failed to download the base image; Go compilation has not started. Check registry connectivity and credentials; use DISTVIS_GO_IMAGE and DISTVIS_RUNTIME_IMAGE to point to reachable base images.';
  } else if (/permission denied.*docker|Cannot connect to the Docker daemon|docker daemon is not running|ENOENT/i.test(text)) {
    hint = 'Docker is not reachable. Make sure Docker Desktop is running, the docker command works, and this process may access the Docker daemon.';
  } else {
    hint = 'Image build failed. Open the event details and check the full output below for Go compile errors or Docker errors.';
  }
  return `${hint}\n\n${text}`;
}

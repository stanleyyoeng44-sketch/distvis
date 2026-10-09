import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildImages, dockerfile, describeBuildFailure } from '../server/build.js';

test('default FROM instructions use explicit registry endpoints instead of the configured Hub mirror', () => {
  const from = dockerfile(buildImages({})).split('\n').filter(line => line.startsWith('FROM '));
  assert.deepEqual(from, [
    'FROM registry-1.docker.io/library/golang:1.24-alpine AS build',
    'FROM registry-1.docker.io/library/alpine:3.21',
  ]);
});

test('custom registries, ports and digest-pinned images are supported without allowing Dockerfile injection', () => {
  const images = buildImages({
    DISTVIS_GO_IMAGE: 'registry.example.edu:5000/course/go:1.24',
    DISTVIS_RUNTIME_IMAGE: `registry.example.edu/course/alpine@sha256:${'a'.repeat(64)}`,
  });
  assert.ok(dockerfile(images).includes(`FROM ${images.runtime}\n`));
  for (const value of ['golang:1.24\nRUN echo unexpected', 'golang:1.24 AS other', '${IMAGE}', '-bad']) {
    assert.throws(() => buildImages({ DISTVIS_GO_IMAGE: value }), /不是合法的镜像引用/);
  }
});

test('403 placeholder-mirror failures explain the actual fix and preserve the build log', () => {
  const output = 'docker build 失败 (1): failed to resolve source metadata: unexpected status from HEAD request to https://yourcode.mirror.aliyuncs.com/v2/library/alpine/manifests/3.21: 403 Forbidden';
  const result = describeBuildFailure(new Error(output));
  assert.match(result, /占位地址/);
  assert.match(result, /registry-mirrors/);
  assert.ok(result.endsWith(output));
});

test('registry download and Go compilation failures get different diagnostics', () => {
  assert.match(describeBuildFailure(new Error('failed to resolve source metadata: dial tcp: no such host')), /基础镜像下载失败/);
  const compiler = '#2 [internal] load metadata for registry-1.docker.io/library/golang:1.24-alpine\n#2 DONE 0.0s\n#7 cmd/node-1/main.go:4:2: undefined: missing';
  const result = describeBuildFailure(new Error(compiler));
  assert.doesNotMatch(result, /基础镜像下载失败/);
  assert.ok(result.endsWith(compiler));
  assert.match(describeBuildFailure(new Error('permission denied while trying to connect to the docker API')), /无法访问 Docker/);
});

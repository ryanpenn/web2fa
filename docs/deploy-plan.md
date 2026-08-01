# Forgejo CI 与 Docker 发布实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `web2fa` 增加以 `VERSION` 变更为发布信号的 Forgejo Actions 工作流，在指定主机部署隔离的 `ubuntu-runner`，将版本化 Docker 镜像通过 SSH 发布到目标服务器，并由 Caddy 以 `https://dev.pomeva.cn/2fa/` 对外提供服务。

**Architecture:** Forgejo Runner 部署在 `124.222.255.65`，通过独立 Docker-in-Docker daemon 构建镜像，不挂载宿主机 `/var/run/docker.sock`。CI 在 `master` 分支的 `VERSION` 文件发生变化时构建 `linux/amd64` 镜像，将镜像归档和 Compose 配置通过 SSH 发送到 `TARGET_SERVER_HOST` 的 `/home/ubuntu/pomeva/web2fa/`；目标 Compose 通过受 Git 管理的 `compose.caddy.yaml` 将应用接入既有 `pomeva-net`。Caddy 配置以 `_servers/server-sh-geet` 为唯一权威：主 `Caddyfile` 声明 `dev.pomeva.cn` 并显式导入 `dev_web2fa_routes`，`caddy/caddy.d/web2fa.caddy` 定义 `/2fa` 路由；前端使用相对 URL，同时兼容直接访问 `:8081` 和公开 `/2fa/` 路径。

**Tech Stack:** Go 1.25.4、Gin、Docker 多阶段构建、Docker Compose、Forgejo Actions、Forgejo Runner 12.7.2、POSIX shell、SSH/SCP。

## Global Constraints

- 当前阶段只执行计划编写；必须得到用户确认后才能修改 CI、连接服务器、注册 runner、提交或推送。
- `VERSION` 初始内容必须严格为 `v1.0.0`，末尾保留一个换行符。
- 自动发布只允许由 `master` 分支上的 `VERSION` 变更触发；另提供 `workflow_dispatch` 供首次配置后的人工验收。
- 发布参数使用 Forgejo 仓库级 Actions Secrets：`TARGET_SERVER_HOST`、`TARGET_SERVER_PORT`、`TARGET_SERVER_USER`、`TARGET_SERVER_KEY`。
- `TARGET_SERVER_PORT` 未配置或为空时使用 `22`。
- `TARGET_SERVER_KEY`、Forgejo Runner UUID/TOKEN 不进入 Git、不写入 CI 日志、不出现在提交信息中。
- runner 主机固定为 `ubuntu@124.222.255.65`，登录密钥固定为本机 `~/.ssh/id_ed25519_geet_pomeva_cn`。
- runner 主机与发布目标服务器是两个独立角色；除非 `TARGET_SERVER_HOST` 最终填写为 `124.222.255.65`，不得假设它们是同一台主机。
- 本次 `dev.pomeva.cn/2fa` 拓扑要求 `TARGET_SERVER_HOST` 最终指向 `124.222.255.65`，因为 Caddy 与 web2fa 必须共享该主机的 `pomeva-net`；若填写其他主机，CI 必须在部署前失败，不能退化为跨主机 Docker DNS 假设。
- 目标服务器的固定工作目录为 `/home/ubuntu/pomeva/web2fa/`；工作流、发布脚本、回滚和验收不得改用 `$HOME` 推导其他目录。
- Caddy 公开入口固定为 `https://dev.pomeva.cn/2fa/`；`/2fa` 必须以 `308` 重定向到 `/2fa/`，保证相对 URL 的目录基准正确。
- `dev.pomeva.cn` 当前仍在 DNS 解析流程中、暂时不可访问；DNS 未指向目标服务器前，只完成容器、上游和 Caddy 配置验收，不把公网探针失败判定为应用发布失败。
- Caddy 配置权威固定为 `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet`，服务器部署权威路径固定为 `/home/ubuntu/pomeva/caddy/`；不得由 web2fa CI 直接生成或改写服务器 Caddyfile。
- 必须先完成并验收 `server-sh-geet/caddy-config-plan.md` 的主配置/`caddy.d` 拆分，再实施 web2fa Caddy 路由；当前读取基线显示该拆分尚未落地，不能把拆分与新增 `dev.pomeva.cn` 合并为一次变更。
- web2fa Caddy 变更只创建命名 snippet `(dev_web2fa_routes)`，主 `Caddyfile` 负责 `dev.pomeva.cn` 站点声明、公共压缩/日志和显式 `import dev_web2fa_routes`；不得在子配置中创建重复站点块。
- 正式 Compose 中主 `Caddyfile` 保持 `:ro`，`caddy.d` 按新规则挂载为 `:rw`；候选和离线验证容器中的两者都使用 `:ro`，验证过程不得写回权威配置。
- Caddy 仅监听 TCP `80/443` 并保持 `protocols h1 h2`；不得新增 UDP `443`、HTTP/3 或计划外 published port，不得移动或清空 `caddy/data` 与 `caddy/config`。
- `_servers` 配置变更与 web2fa 源码是两个独立 Git 变更集，分别精确暂存；修改、提交、推送 `_servers` 必须获得对应范围的明确授权。
- 构建产物固定为 `linux/amd64`；发布前检查目标 Docker 架构，非 `x86_64/amd64` 时停止发布。
- runner 容量设为 `1`，Docker-in-Docker API 只允许在 runner 专用 Compose 网络中访问。
- 不引入新的 Go 或前端依赖，不改变应用路由与页面行为。

---

## 文件结构

- Create: `VERSION` — 唯一发布版本源。
- Create: `.forgejo/workflows/deploy.yml` — VERSION 触发、构建、传输、发布和验收工作流。
- Create: `scripts/validate-version.sh` — 在本地和 CI 中复用的版本格式校验。
- Create: `scripts/deploy-remote.sh` — 在目标服务器执行版本切换、健康检查和失败回滚。
- Create: `compose.caddy.yaml` — 目标服务器 Compose override，将 `web2fa` 接入外部网络 `pomeva-net`。
- Modify: `compose.yaml` — 将镜像名改为 `${WEB2FA_IMAGE:-web2fa:local}`，保持本地默认行为不变。
- Modify: `.dockerignore` — 排除 `.git`、`docs`、构建归档和 runner/CI 临时文件。
- Modify: `web/index.html` — 将导航、动态码请求和 Secret 链接改为路径相对 URL，使 `/2fa/` 前缀下的交互不跳回站点根路径。
- Modify: `main_test.go` — 增加子路径兼容标记测试并更新 Secret 链接断言。
- Remote create: `/opt/forgejo-runner/compose.yaml` — runner 与 Docker-in-Docker 服务定义。
- Remote create: `/opt/forgejo-runner/data/runner-config.yml` — runner label、DIND 连接和 Forgejo 凭据；仅保存在 runner 主机。
- Remote create: `/opt/forgejo-runner/data/secrets/forgejo-token` — runner TOKEN；mode `0600`，不内联到 Compose 或 runner 配置。
- Cross-repo modify: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/caddy/Caddyfile` — 增加 `dev.pomeva.cn` 站点并显式导入 web2fa snippet。
- Cross-repo create: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/caddy/caddy.d/web2fa.caddy` — 定义 `(dev_web2fa_routes)`。
- Cross-repo modify: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/readme.md`, `deploy.md`, `runbook.md` — 登记新 snippet、网络和验证/回滚方法。
- Remote deploy from authority: `/home/ubuntu/pomeva/caddy/Caddyfile`, `/home/ubuntu/pomeva/caddy/caddy.d/web2fa.caddy` — 只从 `_servers` 候选发布，不在服务器临时手改。

### Task 1: 建立本地与远端基线

**Files:**

- Read: `Dockerfile`
- Read: `compose.yaml`
- Read: `go.mod`
- Read: `.git/config`

**Interfaces:**

- Consumes: 当前 Git 工作树、Dockerfile、Compose 配置和已授权 SSH 入口。
- Produces: 可审计的本地/远端环境清单，确认 Caddy 权威、`pomeva-net`、容器名和主配置/子配置拆分前置状态；不产生任何配置变更。

- [ ] **Step 1: 确认本地工作树与远端仓库**

  Run:

  ```bash
  git status --short --branch
  git remote -v
  git branch --show-current
  ```

  Expected: 分支为 `master`，远端为 `ssh://git@geet.pomeva.cn:33/pomeva-team/web2fa.git`；若工作树存在非本任务修改，记录并仅暂存本计划列出的文件。

- [ ] **Step 2: 对 runner 主机做只读盘点**

  Run:

  ```bash
  ssh -i ~/.ssh/id_ed25519_geet_pomeva_cn ubuntu@124.222.255.65 \
    'set -eu; uname -a; uname -m; id; docker version; docker compose version; df -h / /opt; docker ps --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"; sudo ss -lntp'
  ```

  Expected: 主机可连接，Docker 与 Compose 可用，`/opt` 空间充足，并明确记录现有容器和监听端口。任一前提不满足时停止写入远端。

- [ ] **Step 3: 核对 Forgejo 实例与 Actions 状态**

  Run:

  ```bash
  curl --fail --silent --show-error --location https://geet.pomeva.cn/api/v1/version
  ```

  Expected: 返回 Forgejo 版本 JSON。随后在仓库设置中确认 Actions 已启用，提供的 UUID/TOKEN 对应 `pomeva-team/web2fa` 可见的 runner 作用域。

- [ ] **Step 4: 记录 dev.pomeva.cn 当前 DNS 状态**

  Run:

  ```bash
  dig +short A dev.pomeva.cn
  dig +short AAAA dev.pomeva.cn
  curl --noproxy '*' --head --location --connect-timeout 5 --max-time 10 https://dev.pomeva.cn/2fa/ || true
  ```

  Expected: 记录当前 A/AAAA 和 HTTPS 结果。域名尚未解析或不可访问是已知前置状态，不在此步骤修改 DNS，也不因此阻断 runner 与容器部署。

- [ ] **Step 5: 核对新的 Caddy 配置权威与拆分前置状态**

  Run locally:

  ```bash
  cd /Users/ryanpenn/Workspace/Ai/cc-workspace/_servers
  git status --short --branch
  test -f server-sh-geet/caddy/Caddyfile
  test -f server-sh-geet/caddy/caddy.d/forgejo.caddy
  test -f server-sh-geet/caddy/caddy.d/mihomo.caddy
  rg -n 'import caddy\.d/\*\.caddy|import geet_mihomo_routes|import geet_forgejo_routes' \
    server-sh-geet/caddy/Caddyfile
  rg -n './caddy/caddy\.d:/etc/caddy/caddy\.d:rw|name: pomeva-net' \
    server-sh-geet/docker-compose.yml
  ```

  Run on `124.222.255.65`:

  ```bash
  ssh -i ~/.ssh/id_ed25519_geet_pomeva_cn ubuntu@124.222.255.65 \
    'set -eu; cd /home/ubuntu/pomeva; docker compose ps caddy forgejo mihomo; test -f caddy/Caddyfile; test -f caddy/caddy.d/forgejo.caddy; test -f caddy/caddy.d/mihomo.caddy; docker inspect server-sh-geet-caddy-1 --format "{{range .Mounts}}{{println .Source \"->\" .Destination .RW}}{{end}}"; docker network inspect pomeva-net --format "{{.Name}}"; docker compose exec -T caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile'
  ```

  Expected: 本地和服务器均已完成 `caddy.d` 拆分，主配置显式编排 Mihomo 后 Forgejo；`/etc/caddy/Caddyfile` mount 的 `RW` 为 `false`，`/etc/caddy/caddy.d` mount 的 `RW` 为 `true`；Caddy 容器为 `server-sh-geet-caddy-1`，共享网络为 `pomeva-net`。任一检查失败时，Task 8 保持 blocked，先单独实施并验收 `server-sh-geet/caddy-config-plan.md`；不得在 web2fa 任务中顺手完成拆分。当前文档编写时的已知状态是本地 `caddy.d` 尚不存在。

### Task 2: 增加 VERSION 和可复用版本校验

**Files:**

- Create: `VERSION`
- Create: `scripts/validate-version.sh`

**Interfaces:**

- Consumes: 仓库根目录中的 `VERSION` 文本。
- Produces: `scripts/validate-version.sh [VERSION_FILE]`；成功时输出规范版本并返回 `0`，格式错误时返回非零。

- [ ] **Step 1: 先写版本校验脚本的失败用例**

  Run:

  ```bash
  test_dir="$(mktemp -d)"
  printf '%s\n' '1.0.0' > "$test_dir/VERSION"
  ! sh scripts/validate-version.sh "$test_dir/VERSION"
  rm -r "$test_dir"
  ```

  Expected: 脚本尚不存在，因此用例失败；实现后，非法版本必须被拒绝。

- [ ] **Step 2: 创建 VERSION 与校验脚本**

  `VERSION` 内容：

  ```text
  v1.0.0
  ```

  `scripts/validate-version.sh` 必须使用 `set -eu`，默认读取 `VERSION`，只接受正则 `^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`，并拒绝空行、多行和前后空白。

- [ ] **Step 3: 验证合法与非法版本**

  Run:

  ```bash
  sh scripts/validate-version.sh VERSION
  test_dir="$(mktemp -d)"
  printf '%s\n' '1.0.0' > "$test_dir/VERSION"
  ! sh scripts/validate-version.sh "$test_dir/VERSION"
  printf '%s\n' 'v1.0.0 beta' > "$test_dir/VERSION"
  ! sh scripts/validate-version.sh "$test_dir/VERSION"
  rm -r "$test_dir"
  ```

  Expected: `VERSION` 输出 `v1.0.0`；两个非法输入均返回非零。

### Task 3: 让现有 Compose 支持版本化镜像

**Files:**

- Modify: `compose.yaml`
- Create: `compose.caddy.yaml`
- Modify: `.dockerignore`

**Interfaces:**

- Consumes: 可选环境变量 `WEB2FA_IMAGE`，以及目标服务器现有外部网络 `pomeva-net`。
- Produces: 基础 Compose 未配置时继续构建/运行 `web2fa:local`；目标部署叠加 `compose.caddy.yaml` 后运行指定版本镜像并以 DNS 名 `web2fa:8081` 供 Caddy 访问。

- [ ] **Step 1: 修改镜像插值**

  将服务镜像行改为：

  ```yaml
  image: ${WEB2FA_IMAGE:-web2fa:local}
  ```

  其余端口、安全选项和重启策略保持不变。

- [ ] **Step 2: 收紧 Docker 构建上下文**

  `.dockerignore` 至少包含：

  ```text
  .git
  .forgejo
  docs
  dist
  *.tar
  *.tar.gz
  ```

- [ ] **Step 3: 创建目标服务器网络 override**

  `compose.caddy.yaml` 的完整内容：

  ```yaml
  services:
    web2fa:
      networks:
        - pomeva-net

  networks:
    pomeva-net:
      external: true
      name: pomeva-net
  ```

  该文件进入 web2fa Git 仓库并随发布包上传，不在目标服务器现场生成。基础 Compose 不引用外部网络，因此本地开发不依赖 `pomeva-net`。

- [ ] **Step 4: 验证默认值、版本覆盖和目标网络**

  Run:

  ```bash
  docker compose config --quiet
  docker compose config | rg 'image: web2fa:local'
  WEB2FA_IMAGE=web2fa:v1.0.0 docker compose config | rg 'image: web2fa:v1.0.0'
  WEB2FA_IMAGE=web2fa:v1.0.0 \
    docker compose -f compose.yaml -f compose.caddy.yaml config --quiet
  WEB2FA_IMAGE=web2fa:v1.0.0 \
    docker compose -f compose.yaml -f compose.caddy.yaml config --format json \
    | python3 -c '
  import json, sys
  cfg = json.load(sys.stdin)
  assert cfg["networks"]["pomeva-net"]["external"] is True
  assert "pomeva-net" in cfg["services"]["web2fa"]["networks"]
  print("web2fa caddy network: ok")
  '
  ```

  Expected: 基础 Compose 与目标叠加配置均解析成功，默认与覆盖后的镜像名分别匹配，仅输出 `web2fa caddy network: ok`。真实 `pomeva-net` 存在性在目标服务器验证。

### Task 4: 兼容 Caddy 的 /2fa 子路径

**Files:**

- Modify: `web/index.html`
- Modify: `main_test.go`

**Interfaces:**

- Consumes: 浏览器当前地址目录；后端仍接收 `/`、`/list`、`/:secret` 和 `/generate`。
- Produces: 在直接访问 `http://host:8081/` 和 Caddy 入口 `https://dev.pomeva.cn/2fa/` 下均正确工作的导航、动态码请求与 Secret 链接。

- [ ] **Step 1: 先写子路径兼容失败测试**

  在 `main_test.go` 增加 `TestRenderedPageUsesPathRelativeURLs`，从 `/list` 渲染页面并断言包含：

  ```go
  wants := []string{
      `data-route="home" href="./"`,
      `data-route="list" href="list"`,
      `fetch("generate?secret=" + encodeURIComponent(secret))`,
      `secretLink.setAttribute("href", encodeURIComponent(record.secret));`,
  }
  ```

  同时断言页面不包含以下根路径绝对 URL：

  ```go
  forbidden := []string{
      `data-route="home" href="/"`,
      `data-route="list" href="/list"`,
      `fetch("/generate?secret="`,
      `secretLink.setAttribute("href", "/" +`,
  }
  ```

- [ ] **Step 2: 运行测试确认当前实现失败**

  Run:

  ```bash
  go test ./... -run TestRenderedPageUsesPathRelativeURLs -count=1
  ```

  Expected: FAIL，指出当前页面仍含根路径绝对 URL。

- [ ] **Step 3: 将四处 URL 改为相对地址**

  在 `web/index.html` 进行以下精确替换：

  ```html
  <a data-route="home" href="./">单个动态码</a>
  <a data-route="list" href="list">动态码列表</a>
  ```

  ```javascript
  const response = await fetch("generate?secret=" + encodeURIComponent(secret));
  secretLink.setAttribute("href", encodeURIComponent(record.secret));
  ```

  不增加 `<base>` 标签，不修改 Gin 路由；Caddy 剥离 `/2fa` 后，后端继续看到原路由。

- [ ] **Step 4: 更新现有 Secret 链接测试并运行全量测试**

  将 `TestListSecretLinksToSecretPage` 的旧断言更新为：

  ```go
  `secretLink.setAttribute("href", encodeURIComponent(record.secret));`,
  ```

  Run:

  ```bash
  go test ./...
  ```

  Expected: 所有测试通过；直接根路径行为不变，页面不再生成会跳出 `/2fa/` 的 URL。

### Task 5: 实现目标服务器发布与回滚脚本

**Files:**

- Create: `scripts/deploy-remote.sh`

**Interfaces:**

- Consumes: 环境变量 `RELEASE_VERSION`、当前目录中的 `compose.yaml`、`compose.caddy.yaml` 和 `web2fa-${RELEASE_VERSION}.tar.gz`。
- Produces: `/home/ubuntu/pomeva/web2fa/current.env`、`compose.yaml`、`compose.caddy.yaml`、已接入 `pomeva-net` 的 `web2fa` Compose 服务，以及失败时恢复的上一版本。

- [ ] **Step 1: 编写静态失败用例**

  Run:

  ```bash
  sh -n scripts/deploy-remote.sh
  ! RELEASE_VERSION=1.0.0 sh scripts/deploy-remote.sh
  ```

  Expected: 实现前脚本不存在；实现后 shell 语法通过，缺少 `v` 前缀的版本在触碰 Docker 前被拒绝。

- [ ] **Step 2: 实现发布事务**

  脚本使用 POSIX `sh` 和 `set -eu`，将 `DEPLOY_DIR` 固定为 `/home/ubuntu/pomeva/web2fa`，并按以下固定顺序执行：

  1. 根据脚本路径解析本次发布目录，调用同目录随包上传的 `validate-version.sh` 校验 `RELEASE_VERSION`。
  2. 检查 `docker info --format '{{.Architecture}}'` 为 `x86_64` 或 `amd64`，并以 `docker network inspect pomeva-net` 验证 Caddy 共享网络已存在。
  3. 从本次发布目录执行 `gzip -dc "web2fa-${RELEASE_VERSION}.tar.gz" | docker load`。
  4. 检查 `docker image inspect "web2fa:${RELEASE_VERSION}"` 成功。
  5. 在 `/home/ubuntu/pomeva/web2fa` 保存旧 `current.env` 为 `rollback.env`，保存旧 `compose.yaml` 为 `compose.rollback.yaml`，若旧 `compose.caddy.yaml` 存在则保存为 `compose.caddy.rollback.yaml`。
  6. 将本次发布目录的 `compose.yaml` 和 `compose.caddy.yaml` 分别复制为目标目录中的 `.tmp` 文件，执行叠加 `docker compose ... config --quiet` 后原子替换两个权威文件；原子写入 `current.env.tmp`，内容为 `WEB2FA_IMAGE=web2fa:${RELEASE_VERSION}`，再替换 `current.env`。
  7. 在 `/home/ubuntu/pomeva/web2fa` 执行 `docker compose --project-name web2fa --env-file current.env -f compose.yaml -f compose.caddy.yaml up -d --no-build --remove-orphans`，再用 `docker inspect web2fa` 确认容器加入 `pomeva-net`。
  8. 最多等待 30 秒，每 2 秒请求一次 `http://127.0.0.1:8081/`；成功后再检查 `/list` 与 `/generate?secret=JBSWY3DPEHPK3PXP`。
  9. 若健康检查失败且存在 `rollback.env`，同时恢复旧环境文件、基础 Compose 和 Caddy override，并使用 Step 7 的固定双 Compose 参数再次执行；回滚后仍以非零状态退出，使 Action 明确失败。
  10. 成功后删除本次发布目录中的 `.tar.gz`，保留当前镜像和上一个镜像用于人工回滚。

- [ ] **Step 3: 验证脚本不会接受异常输入**

  Run:

  ```bash
  sh -n scripts/deploy-remote.sh
  ! RELEASE_VERSION='../bad' sh scripts/deploy-remote.sh
  ! RELEASE_VERSION='v1.0.0 bad' sh scripts/deploy-remote.sh
  ```

  Expected: 语法检查通过，路径穿越和带空白版本均在任何 Docker 操作前失败。

### Task 6: 增加 Forgejo Actions 工作流

**Files:**

- Create: `.forgejo/workflows/deploy.yml`

**Interfaces:**

- Consumes: `VERSION`、Dockerfile、Compose、发布脚本和四个仓库级 Actions Secrets。
- Produces: `web2fa:${VERSION}` 的 `linux/amd64` 镜像和一次目标服务器部署。

- [ ] **Step 1: 配置严格触发条件**

  工作流触发头必须为：

  ```yaml
  name: Build and deploy web2fa

  on:
    push:
      branches:
        - master
      paths:
        - VERSION
    workflow_dispatch:

  concurrency:
    group: web2fa-production
    cancel-in-progress: false
  ```

  这保证普通代码提交不会发布，只有 `master` 的 `VERSION` 变化自动触发；并发发布排队执行，不允许后一次发布中断正在进行的部署。

- [ ] **Step 2: 配置 runner 和最小权限 checkout**

  Job 使用 `runs-on: ubuntu-runner`、`timeout-minutes: 30`，checkout 固定为完全限定地址：

  ```yaml
  - uses: https://data.forgejo.org/actions/checkout@v6
    with:
      persist-credentials: false
  ```

  Job 镜像由 runner label 固定为含 Node.js 的 Debian Bookworm；工作流只安装 `docker.io`、`openssh-client`、`curl`、`gzip` 和 `ca-certificates`。

- [ ] **Step 3: 增加 Secrets 预检**

  将四个 Secrets 映射到 step 级 `env`。脚本逐项只检查是否为空，不打印值；端口用 `${TARGET_SERVER_PORT:-22}`。任何缺失项均输出变量名并失败。

- [ ] **Step 4: 构建并归档版本化镜像**

  Run:

  ```bash
  set -eu
  version="$(sh scripts/validate-version.sh VERSION)"
  docker build \
    --platform linux/amd64 \
    --label "org.opencontainers.image.version=$version" \
    --label "org.opencontainers.image.revision=$FORGEJO_SHA" \
    --tag "web2fa:$version" \
    .
  docker image inspect "web2fa:$version"
  docker save "web2fa:$version" | gzip -9 > "web2fa-$version.tar.gz"
  test -s "web2fa-$version.tar.gz"
  ```

  Expected: Dockerfile 多阶段构建成功，归档非空，运行时仍为 `scratch` 镜像。

- [ ] **Step 5: 建立 SSH 信任并传输发布包**

  使用临时目录保存私钥，`trap` 在 job 结束时删除；`ssh-keyscan -p "$TARGET_SERVER_PORT" -H "$TARGET_SERVER_HOST"` 写入临时 `known_hosts`。先通过 SSH 检查 `/home/ubuntu/pomeva/web2fa` 存在、可写，确认 `pomeva-net` 存在，并创建 `/home/ubuntu/pomeva/web2fa/incoming/${VERSION}`；再通过 `scp -P` 上传镜像归档、`compose.yaml`、`compose.caddy.yaml`、`validate-version.sh` 和 `deploy-remote.sh` 到该版本目录。

  首次实施时必须从可信渠道核对 `ssh-keyscan` 输出的主机指纹；当前四变量契约未提供固定 host key，因此这一点属于首次上线人工门禁。

- [ ] **Step 6: 远端执行并回传验收证据**

  通过 `ssh -p` 进入 `/home/ubuntu/pomeva/web2fa/incoming/${VERSION}`，以 `RELEASE_VERSION="$version" sh deploy-remote.sh` 发布。随后输出但不泄露凭据的证据：工作目录、容器名、镜像名、状态、端口映射、`org.opencontainers.image.version` 和三个 HTTP 探针状态。

- [ ] **Step 7: 静态校验工作流**

  Run:

  ```bash
  rg -n 'branches:|paths:|VERSION|runs-on: ubuntu-runner|TARGET_SERVER_(HOST|PORT|USER|KEY)|persist-credentials: false' .forgejo/workflows/deploy.yml
  ! rg -n 'server\.connections|uuid:[[:space:]]|token:[[:space:]]|id_ed25519_geet_pomeva_cn' .forgejo VERSION scripts compose.yaml compose.caddy.yaml
  ```

  Expected: 触发条件、runner label 和四个 Secrets 引用完整，仓库文件中不存在 runner UUID、TOKEN 或本机私钥路径。

### Task 7: 在 124.222.255.65 部署 ubuntu-runner

**Files:**

- Remote create: `/opt/forgejo-runner/compose.yaml`
- Remote create: `/opt/forgejo-runner/data/runner-config.yml`

**Interfaces:**

- Consumes: `https://geet.pomeva.cn/`、用户已提供的 runner UUID/TOKEN。
- Produces: Forgejo 中在线、label 为 `ubuntu-runner`、capacity 为 `1` 的持久 runner。

- [ ] **Step 1: 创建专用目录和权限**

  在 runner 主机执行：

  ```bash
  sudo install -d -m 0750 /opt/forgejo-runner
  sudo install -d -o 1001 -g 1001 -m 0750 /opt/forgejo-runner/data
  sudo install -d -o 1001 -g 1001 -m 0770 /opt/forgejo-runner/data/.cache
  ```

- [ ] **Step 2: 生成与填写 runner 配置**

  先用 `data.forgejo.org/forgejo/runner:12.7.2` 执行 `forgejo-runner generate-config` 核对当前 schema，再创建以下最小配置；`${FORGEJO_RUNNER_UUID}` 由执行时的受限 shell 环境展开，不把实际值写入计划或命令历史：

  ```yaml
  log:
    level: info
    job_level: info

  runner:
    file: /data/.runner
    capacity: 1
    envs:
      DOCKER_HOST: tcp://dind.docker.internal:2375
    timeout: 3h
    shutdown_timeout: 3h
    insecure: false
    fetch_timeout: 5s
    fetch_interval: 2s
    report_interval: 1s
    labels:
      - ubuntu-runner:docker://data.forgejo.org/oci/node:22-bookworm

  cache:
    enabled: false

  container:
    network: ""
    enable_ipv6: false
    privileged: false
    options: --add-host=dind.docker.internal:host-gateway
    workdir_parent:
    valid_volumes: []
    docker_host: tcp://docker-in-docker:2375
    force_pull: true
    force_rebuild: false

  host:
    workdir_parent:

  server:
    connections:
      forgejo:
        url: https://geet.pomeva.cn/
        uuid: ${FORGEJO_RUNNER_UUID}
        token_url: file:/data/secrets/forgejo-token
  ```

  TOKEN 通过无回显 stdin 写入 `/opt/forgejo-runner/data/secrets/forgejo-token`，不内联到 YAML。`runner-config.yml` 与 token 文件 owner 均为 `1001:1001`、mode 均为 `0600`；完成后立即清除临时 shell 变量。

- [ ] **Step 3: 部署隔离的 DIND Compose**

  `/opt/forgejo-runner/compose.yaml` 使用已验证存在的固定镜像版本：

  ```yaml
  name: forgejo-runner

  services:
    docker-in-docker:
      image: docker:28.5.2-dind
      privileged: true
      command:
        - dockerd
        - -H
        - tcp://0.0.0.0:2375
        - --tls=false
      networks:
        - runner
      volumes:
        - dind-data:/var/lib/docker
      healthcheck:
        test: [CMD, docker, info]
        interval: 5s
        timeout: 5s
        retries: 20
      restart: unless-stopped

    runner:
      image: data.forgejo.org/forgejo/runner:12.7.2
      user: 1001:1001
      working_dir: /data
      depends_on:
        docker-in-docker:
          condition: service_healthy
      environment:
        DOCKER_HOST: tcp://docker-in-docker:2375
      networks:
        - runner
      volumes:
        - ./data:/data
      command: forgejo-runner daemon --config /data/runner-config.yml
      restart: unless-stopped

  networks:
    runner:

  volumes:
    dind-data:
  ```

  DIND API 不发布宿主端口，runner 不挂载宿主 Docker socket；`privileged` 仅授予 DIND 容器。

- [ ] **Step 4: 启动并验证 runner**

  Run on runner host:

  ```bash
  cd /opt/forgejo-runner
  sudo docker compose config --quiet
  sudo docker compose pull
  sudo docker compose up -d
  sudo docker compose ps
  sudo docker compose logs --tail=100 runner
  sudo ss -lntp | rg ':2375' && exit 1 || true
  ```

  Expected: runner 与 DIND 均为运行状态，日志显示已连接 Forgejo，宿主机没有监听 `2375`。在 Forgejo 仓库设置中确认 runner 在线且 label 为 `ubuntu-runner`。

### Task 8: 通过 server-sh-geet 配置权威发布 Caddy 路由

**Files:**

- Cross-repo modify: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/caddy/Caddyfile`
- Cross-repo create: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/caddy/caddy.d/web2fa.caddy`
- Cross-repo modify: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/readme.md`
- Cross-repo modify: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/deploy.md`
- Cross-repo modify: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/runbook.md`
- Cross-repo modify after successful deployment: `/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/deploy-plan.md`
- Remote candidate: `/home/ubuntu/pomeva/caddy/.candidate-web2fa/`
- Remote backup: `/home/ubuntu/pomeva/caddy/backups/config-before-web2fa.tar.gz`

**Interfaces:**

- Consumes: 已完成的 Caddy 主配置/`caddy.d` 拆分、共享 Docker 网络 `pomeva-net`、上游 `web2fa:8081`。
- Produces: 命名 snippet `(dev_web2fa_routes)`，主配置中的 `dev.pomeva.cn` site block，`/2fa` 到 `/2fa/` 的 `308` 重定向和剥离前缀后的反向代理。

- [ ] **Step 1: 执行拆分前置门禁并保存基线**

  Run:

  ```bash
  cd /Users/ryanpenn/Workspace/Ai/cc-workspace/_servers
  git status --short --branch
  test -f server-sh-geet/caddy/caddy.d/forgejo.caddy
  test -f server-sh-geet/caddy/caddy.d/mihomo.caddy
  rg -n 'import caddy\.d/\*\.caddy|import geet_mihomo_routes|import geet_forgejo_routes' \
    server-sh-geet/caddy/Caddyfile
  sha256sum \
    server-sh-geet/docker-compose.yml \
    server-sh-geet/caddy/Caddyfile \
    server-sh-geet/caddy/caddy.d/forgejo.caddy \
    server-sh-geet/caddy/caddy.d/mihomo.caddy
  ```

  Expected: 拆分方案已经作为独立变更完成，工作树没有不明修改，主配置与两个既有 snippet 齐全。当前已知基线不满足此条件，因此实施时必须先停止并单独完成 `server-sh-geet/caddy-config-plan.md`；其未跟踪文件也不得被 web2fa 提交误暂存。

- [ ] **Step 2: 创建 web2fa snippet 并显式编排站点**

  `server-sh-geet/caddy/caddy.d/web2fa.caddy` 的完整内容：

  ```caddyfile
  (dev_web2fa_routes) {
      redir /2fa /2fa/ 308

      handle_path /2fa/* {
          reverse_proxy web2fa:8081
      }
  }
  ```

  在主 `Caddyfile` 的既有 `geet.pomeva.cn` site block 之后增加：

  ```caddyfile
  dev.pomeva.cn {
      encode zstd gzip

      import dev_web2fa_routes

      log {
          output stdout
          format console
      }
  }
  ```

  顶层 `import caddy.d/*.caddy` 保持唯一；`web2fa.caddy` 不声明站点块，主配置显式导入确保 snippet 缺失时 fail-closed。保持全局 `protocols h1 h2` 不变。

- [ ] **Step 3: 更新 _servers 权威文档**

  精确更新以下内容：

  - `readme.md`: 文件索引增加 `caddy/caddy.d/web2fa.caddy`，只说明其负责 `dev.pomeva.cn/2fa`。
  - `deploy.md`: 目录树加入 `web2fa.caddy`，网络说明加入上游 `web2fa:8081` 通过 `pomeva-net` 访问。
  - `runbook.md`: 增加 web2fa 上游探针、Caddy 候选验证、主配置变更需 `--force-recreate caddy` 和回滚命令。
  - `deploy-plan.md`: 仅在服务器发布与全部验收成功后记录执行时间、最终 hash、Caddy container ID 和端点结果。

  不修改 Mihomo Secret、订阅 URL、数据库配置或历史记录的原始语境。

- [ ] **Step 4: 执行本地 Compose、adapt、语义增量和 validate 门禁**

  Run:

  ```bash
  cd /Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet
  docker compose config --quiet
  caddy_compare_dir="$(mktemp -d /tmp/server-sh-geet-web2fa.XXXXXX)"
  install -d -m 0750 "$caddy_compare_dir/caddy.d"
  git show HEAD:server-sh-geet/caddy/Caddyfile > "$caddy_compare_dir/Caddyfile.before"
  git show HEAD:server-sh-geet/caddy/caddy.d/forgejo.caddy > "$caddy_compare_dir/caddy.d/forgejo.caddy"
  git show HEAD:server-sh-geet/caddy/caddy.d/mihomo.caddy > "$caddy_compare_dir/caddy.d/mihomo.caddy"
  caddy_image_ref="$(docker compose config --images | awk '/caddy/ {print; exit}')"

  docker run --rm --network none --entrypoint caddy \
    --volume "$caddy_compare_dir/Caddyfile.before:/etc/caddy/Caddyfile:ro" \
    --volume "$caddy_compare_dir/caddy.d:/etc/caddy/caddy.d:ro" \
    "$caddy_image_ref" adapt --config /etc/caddy/Caddyfile --adapter caddyfile --pretty \
    > "$caddy_compare_dir/before.json"

  docker compose run --rm --no-deps --entrypoint caddy caddy \
    adapt --config /etc/caddy/Caddyfile --adapter caddyfile --pretty \
    > "$caddy_compare_dir/after.json"
  docker compose run --rm --no-deps --entrypoint caddy caddy \
    validate --config /etc/caddy/Caddyfile --adapter caddyfile

  python3 - "$caddy_compare_dir/before.json" "$caddy_compare_dir/after.json" <<'PY'
  import json
  import sys

  before_path, after_path = sys.argv[1:]
  with open(before_path, encoding="utf-8") as handle:
      before = json.load(handle)
  with open(after_path, encoding="utf-8") as handle:
      after = json.load(handle)

  def host_routes(config, hostname):
      matches = []
      servers = config.get("apps", {}).get("http", {}).get("servers", {})
      for server in servers.values():
          for route in server.get("routes", []):
              for matcher_set in route.get("match", []):
                  if hostname in matcher_set.get("host", []):
                      matches.append(route)
                      break
      return matches

  def protocols(config):
      servers = config.get("apps", {}).get("http", {}).get("servers", {})
      return sorted(tuple(server.get("protocols", [])) for server in servers.values())

  assert host_routes(before, "dev.pomeva.cn") == []
  assert len(host_routes(after, "dev.pomeva.cn")) == 1
  assert host_routes(before, "geet.pomeva.cn") == host_routes(after, "geet.pomeva.cn")
  assert protocols(before) == protocols(after)
  assert all(items == ("h1", "h2") for items in protocols(after))
  print("Caddy semantic delta: ok")
  PY

  set +e
  diff -u "$caddy_compare_dir/before.json" "$caddy_compare_dir/after.json" \
    > "$caddy_compare_dir/web2fa.diff"
  caddy_diff_rc=$?
  set -e
  test "$caddy_diff_rc" -eq 1
  cat "$caddy_compare_dir/web2fa.diff"
  ```

  Expected: Python 输出 `Caddy semantic delta: ok`；旧配置中不存在 `dev.pomeva.cn` host matcher，新配置中恰有一个；`geet.pomeva.cn` 对应 route JSON 完全相同；所有 HTTP server 的 `protocols` 仍为 `h1/h2`。`diff` 因存在预期增量返回 `1`，人工审查其输出并确认唯一意图增量是 `dev.pomeva.cn` 的路由、日志与 TLS automation policy，随后显式记录审查通过；返回 `2` 或出现其他增量时停止。

- [ ] **Step 5: 验证缺少 web2fa snippet 时 fail-closed**

  在独立临时目录复制候选主配置和既有 `forgejo.caddy`、`mihomo.caddy`，故意不复制 `web2fa.caddy`，使用同一 `caddy_image_ref` 执行：

  ```bash
  caddy_negative_dir="$(mktemp -d /tmp/server-sh-geet-web2fa-negative.XXXXXX)"
  install -d -m 0750 "$caddy_negative_dir/caddy.d"
  install -m 0640 caddy/Caddyfile "$caddy_negative_dir/Caddyfile"
  install -m 0640 caddy/caddy.d/forgejo.caddy caddy/caddy.d/mihomo.caddy \
    "$caddy_negative_dir/caddy.d/"
  set +e
  docker run --rm --network none --entrypoint caddy \
    --volume "$caddy_negative_dir/Caddyfile:/etc/caddy/Caddyfile:ro" \
    --volume "$caddy_negative_dir/caddy.d:/etc/caddy/caddy.d:ro" \
    "$caddy_image_ref" validate --config /etc/caddy/Caddyfile --adapter caddyfile \
    > "$caddy_negative_dir/validate.log" 2>&1
  caddy_negative_rc=$?
  set -e
  test "$caddy_negative_rc" -ne 0
  grep -E 'dev_web2fa_routes|import' "$caddy_negative_dir/validate.log"
  ```

  Expected: 返回非零并明确报告缺少 `dev_web2fa_routes`。只清理本步骤创建且路径通过 `/tmp/server-sh-geet-web2fa*` 前缀校验的临时文件。

- [ ] **Step 6: 同步服务器候选并使用现有镜像强验证**

  在服务器创建固定候选 `/home/ubuntu/pomeva/caddy/.candidate-web2fa`；若已存在则停止并审计，不覆盖。上传候选主配置及 `forgejo.caddy`、`mihomo.caddy`、`web2fa.caddy`，权限统一为 `0640`。随后执行：

  ```bash
  cd /home/ubuntu/pomeva
  caddy_image_ref="$(docker inspect server-sh-geet-caddy-1 --format '{{.Config.Image}}')"
  docker run --rm --pull never --network none --entrypoint caddy \
    --volume /home/ubuntu/pomeva/caddy/.candidate-web2fa/Caddyfile:/etc/caddy/Caddyfile:ro \
    --volume /home/ubuntu/pomeva/caddy/.candidate-web2fa/caddy.d:/etc/caddy/caddy.d:ro \
    "$caddy_image_ref" validate --config /etc/caddy/Caddyfile --adapter caddyfile
  docker network inspect pomeva-net --format '{{.Name}}'
  docker inspect web2fa --format '{{json .NetworkSettings.Networks}}' | grep -F 'pomeva-net'
  ```

  Expected: 不拉取新镜像，候选配置有效，Caddy 与 web2fa 已共享 `pomeva-net`。任一失败均不得发布 Caddy。

- [ ] **Step 7: 等待 DNS 发布门禁**

  Run:

  ```bash
  dig +short A dev.pomeva.cn
  ```

  Expected: 必须包含 `124.222.255.65` 后才进入下一步。域名仍在解析期间，可以完成源码候选和服务器候选验证，但不得安装权威 Caddy 配置、重建 Caddy 或把公网 HTTPS 标记为成功。

- [ ] **Step 8: 创建固定备份并发布权威文件**

  Run on target server:

  ```bash
  cd /home/ubuntu/pomeva
  caddy_backup='caddy/backups/config-before-web2fa.tar.gz'
  test ! -e "$caddy_backup"
  sudo tar --acls --xattrs --numeric-owner -czf "$caddy_backup" \
    caddy/Caddyfile caddy/caddy.d
  sudo chmod 0600 "$caddy_backup"
  sudo sha256sum "$caddy_backup" > "${caddy_backup}.sha256"
  sudo chmod 0600 "${caddy_backup}.sha256"

  install -o ubuntu -g ubuntu -m 0640 \
    caddy/.candidate-web2fa/caddy.d/web2fa.caddy \
    caddy/caddy.d/web2fa.caddy
  install -o ubuntu -g ubuntu -m 0640 \
    caddy/.candidate-web2fa/Caddyfile \
    caddy/Caddyfile
  docker compose config --quiet
  ```

  不覆盖 `caddy/data`、`caddy/config`、`.env`、Forgejo、Mihomo 或 PostgreSQL 配置。

- [ ] **Step 9: 仅受控重建 Caddy 并验证影响范围**

  主 Caddyfile 通过 `install` 替换，按新 runbook 使用受控重建而不是 reload：

  ```bash
  cd /home/ubuntu/pomeva
  caddy_id_before="$(docker compose ps -q caddy)"
  forgejo_id_before="$(docker compose ps -q forgejo)"
  mihomo_id_before="$(docker compose ps -q mihomo)"
  db_id_before="$(docker compose ps -q db)"
  web2fa_id_before="$(docker inspect web2fa --format '{{.Id}}')"
  test -n "$caddy_id_before"
  test -n "$forgejo_id_before"
  test -n "$mihomo_id_before"
  test -n "$db_id_before"
  test -n "$web2fa_id_before"

  docker compose up -d --no-deps --force-recreate \
    --wait --wait-timeout 60 caddy

  caddy_id_after="$(docker compose ps -q caddy)"
  test -n "$caddy_id_after"
  test "$caddy_id_after" != "$caddy_id_before"
  test "$(docker compose ps -q forgejo)" = "$forgejo_id_before"
  test "$(docker compose ps -q mihomo)" = "$mihomo_id_before"
  test "$(docker compose ps -q db)" = "$db_id_before"
  test "$(docker inspect web2fa --format '{{.Id}}')" = "$web2fa_id_before"

  docker compose ps caddy forgejo mihomo db
  docker inspect server-sh-geet-caddy-1 --format \
    '{{range .Mounts}}{{println .Destination .RW}}{{end}}'
  docker compose exec -T caddy \
    caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
  docker compose logs --since 10m caddy
  ```

  Expected: 仅 Caddy container ID/启动时间变化；Forgejo、Mihomo、PostgreSQL 与 web2fa 均未重建；`/etc/caddy/Caddyfile` 的 `RW` 为 `false`，`/etc/caddy/caddy.d` 的 `RW` 为 `true`；Caddy 无 parse、upstream、TLS 或 restart-loop 错误。

- [ ] **Step 10: 完成新旧入口、TLS 和端口验收**

  Run:

  ```bash
  curl --noproxy '*' --fail --silent https://geet.pomeva.cn/api/healthz >/dev/null
  curl --noproxy '*' --silent --show-error --output /dev/null \
    --write-out '%{http_code}\n' https://geet.pomeva.cn/proxy-02bca5d9/ui/
  curl --noproxy '*' --silent --show-error --output /dev/null \
    --write-out '%{http_code}\n' https://geet.pomeva.cn/proxy-02bca5d9/version
  curl --noproxy '*' --silent --show-error --output /dev/null \
    --write-out '%{http_code} %{redirect_url}\n' http://dev.pomeva.cn/2fa
  curl --noproxy '*' --fail --silent --location https://dev.pomeva.cn/2fa/ >/dev/null
  curl --noproxy '*' --fail --silent --location https://dev.pomeva.cn/2fa/list >/dev/null
  curl --noproxy '*' --fail --silent \
    'https://dev.pomeva.cn/2fa/generate?secret=JBSWY3DPEHPK3PXP' \
    | grep -Eq '"code":"[0-9]{6}"'
  openssl s_client -connect dev.pomeva.cn:443 -servername dev.pomeva.cn \
    </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer -dates
  sudo ss -lnuH | awk '{print $5}' | grep -E '(^|:)443$' && exit 1 || true
  ```

  Expected: Forgejo health `200`；Mihomo UI `200`、未鉴权 controller `401`；`/2fa` 经 HTTP/HTTPS 重定向到规范 `/2fa/`；web2fa 三个 HTTPS 探针成功；证书覆盖 `dev.pomeva.cn`；无 UDP `443`。浏览器还需验证单码页、列表页、Secret 跳转、复制和动态码请求始终保留 `/2fa/` 前缀。

- [ ] **Step 11: 记录证据并精确清理候选**

  记录执行时间、Caddy container ID、以下文件 SHA-256 和全部验收结果：

  ```bash
  cd /home/ubuntu/pomeva
  sha256sum caddy/Caddyfile caddy/caddy.d/forgejo.caddy \
    caddy/caddy.d/mihomo.caddy caddy/caddy.d/web2fa.caddy
  ```

  仅在验收成功后，以 `unlink`/`rmdir` 精确清理 `.candidate-web2fa` 中的四个候选文件和目录，并在 `_servers/server-sh-geet/deploy-plan.md` 追加结果；不得清理 `caddy/data`、`caddy/config` 或备份。

- [ ] **Step 12: 单独审查并提交 _servers 变更**

  该步骤与 web2fa 仓库提交完全分离，并且只有在用户明确授权 `_servers` 提交与推送后执行：

  ```bash
  cd /Users/ryanpenn/Workspace/Ai/cc-workspace/_servers
  git status --short --branch
  git diff --check -- \
    server-sh-geet/caddy/Caddyfile \
    server-sh-geet/caddy/caddy.d/web2fa.caddy \
    server-sh-geet/readme.md \
    server-sh-geet/deploy.md \
    server-sh-geet/runbook.md \
    server-sh-geet/deploy-plan.md
  git add \
    server-sh-geet/caddy/Caddyfile \
    server-sh-geet/caddy/caddy.d/web2fa.caddy \
    server-sh-geet/readme.md \
    server-sh-geet/deploy.md \
    server-sh-geet/runbook.md \
    server-sh-geet/deploy-plan.md
  git diff --cached --check
  git diff --cached --stat
  git diff --cached -- \
    server-sh-geet/caddy/Caddyfile \
    server-sh-geet/caddy/caddy.d/web2fa.caddy \
    server-sh-geet/readme.md \
    server-sh-geet/deploy.md \
    server-sh-geet/runbook.md \
    server-sh-geet/deploy-plan.md
  git commit -m "deploy: add web2fa Caddy route"
  git push origin main
  ```

  Expected: 暂存区只包含上述六个权威文件；未跟踪的 `server-sh-geet/caddy-config-plan.md` 和任何其他服务器配置不进入提交。若尚未获得授权，则保留已验证的工作树变更并停在提交前。

### Task 9: 配置发布 Secrets 与端到端验收

**Files:**

- No repository file changes.

**Interfaces:**

- Consumes: 用户在 Forgejo UI 填写的四个 Actions Secrets。
- Produces: 一次可审计的 `v1.0.0` 部署结果。

- [ ] **Step 1: 用户填写仓库级 Actions Secrets**

  在 `pomeva-team/web2fa` 的 `Settings -> Actions -> Secrets` 中填写：

  - `TARGET_SERVER_HOST`: 目标服务器 IP 或可解析主机名
  - `TARGET_SERVER_PORT`: SSH 端口；为空时工作流使用 `22`
  - `TARGET_SERVER_USER`: 具备 Docker 权限的远端用户
  - `TARGET_SERVER_KEY`: 与该用户匹配的完整 OpenSSH 私钥

  目标用户必须可执行 `docker info` 和 `docker compose`，并可写 `/home/ubuntu/pomeva/web2fa/`；不为 CI 开放不受限 sudo。若 `TARGET_SERVER_USER` 不是 `ubuntu`，必须在首次验收前明确授予该目录的最小写权限。

- [ ] **Step 2: 首次手动触发**

  因为添加 `VERSION` 的首次推送可能发生在 Secrets 填写前，用户填写完成后从 Actions 页面使用 `workflow_dispatch` 触发，不需要制造一次无意义版本变更。

- [ ] **Step 3: 核验 Action 与目标服务**

  Expected evidence:

  - Forgejo Action 由在线 `ubuntu-runner` 执行且状态成功。
  - 目标服务器的 Compose、`current.env` 和发布记录均位于 `/home/ubuntu/pomeva/web2fa/`。
  - 目标服务器 `docker inspect web2fa` 显示镜像 `web2fa:v1.0.0`。
  - `http://127.0.0.1:8081/`、`/list` 返回 `200`。
  - `/generate?secret=JBSWY3DPEHPK3PXP` 返回六位动态码 JSON。
  - Caddy 权威配置 validate 成功，主 `Caddyfile` 新增唯一 `dev.pomeva.cn` site block 并显式导入 `(dev_web2fa_routes)`，既有 `geet.pomeva.cn` 路由未变化。
  - DNS 未就绪时，Task 8 Steps 7-12 和 `https://dev.pomeva.cn/2fa/` 验收保持 pending；DNS 就绪后必须完成受控 Caddy 重建、Task 8 Step 10 的公网 HTTPS/TLS 探针和浏览器路径验收。
  - runner 主机无宿主 `/var/run/docker.sock` 挂载，宿主端口 `2375` 未监听。

- [ ] **Step 4: 验证后续自动触发**

  将 `VERSION` 从 `v1.0.0` 提升为下一个真实发布版本并提交到 `master`；确认仅该变更触发自动发布。普通代码提交若未修改 `VERSION`，Actions 不应创建发布任务。

### Task 10: 本地验证、精确提交与推送

**Files:**

- Stage only: `VERSION`, `.forgejo/workflows/deploy.yml`, `scripts/validate-version.sh`, `scripts/deploy-remote.sh`, `compose.yaml`, `compose.caddy.yaml`, `.dockerignore`, `web/index.html`, `main_test.go`, `docs/deploy-plan.md`

**Interfaces:**

- Consumes: Tasks 1-9 的验证结果；DNS 尚未就绪时允许 Task 8 Steps 7-12 保持 pending，但必须完成本地源码验证、runner/容器部署、Caddy 拆分前置门禁和候选配置验证，且不得宣告公网部署完成。
- Produces: 一个范围明确的 Git 提交和推送后的 `origin/master`。

- [ ] **Step 1: 运行完整本地验证**

  Run:

  ```bash
  go test ./...
  go vet ./...
  sh scripts/validate-version.sh VERSION
  sh -n scripts/deploy-remote.sh
  docker compose config --quiet
  docker build --platform linux/amd64 -t web2fa:v1.0.0 .
  rg -n 'href="\./"|href="list"|fetch\("generate\?secret="|setAttribute\("href", encodeURIComponent' web/index.html
  ```

  Expected: 全部命令成功。

- [ ] **Step 2: 检查最终差异与敏感信息**

  Run:

  ```bash
  git diff --check
  git diff -- VERSION .forgejo/workflows/deploy.yml scripts compose.yaml compose.caddy.yaml .dockerignore web/index.html main_test.go docs/deploy-plan.md
  rg -n 'BEGIN (OPENSSH|RSA|EC) PRIVATE KEY|uuid:[[:space:]]|token:[[:space:]]' VERSION .forgejo scripts compose.yaml compose.caddy.yaml .dockerignore web/index.html main_test.go && exit 1 || true
  ```

  Expected: 无空白错误、无范围外变更、无 TOKEN 或私钥内容。

- [ ] **Step 3: 精确暂存并提交**

  Run:

  ```bash
  git add VERSION .forgejo/workflows/deploy.yml scripts/validate-version.sh scripts/deploy-remote.sh compose.yaml compose.caddy.yaml .dockerignore web/index.html main_test.go docs/deploy-plan.md
  git diff --cached --check
  git diff --cached -- VERSION .forgejo/workflows/deploy.yml scripts compose.yaml compose.caddy.yaml .dockerignore web/index.html main_test.go docs/deploy-plan.md
  git commit -m "ci: add Forgejo Docker deployment"
  ```

- [ ] **Step 4: 推送并核对远端**

  Run:

  ```bash
  git push origin master
  git status --short --branch
  git log -1 --oneline
  ```

  Expected: `master` 与 `origin/master` 同步；若 Secrets 尚未填写，首次自动 Action 允许在预检阶段明确失败，待用户填写后以手动触发完成验收。

## 回滚策略

- CI 发布脚本在切换前保存上一版 `current.env`，健康检查失败自动恢复上一镜像并重新执行 Compose。
- `compose.caddy.yaml` 与 `compose.yaml` 同属 web2fa 版本化发布输入；自动或人工回滚必须成对恢复两个文件，不能沿用未知版本的网络覆盖配置。
- 人工回滚时在目标服务器将 `/home/ubuntu/pomeva/web2fa/current.env` 中的 `WEB2FA_IMAGE` 改为上一保留版本，然后运行：

  ```bash
  cd /home/ubuntu/pomeva/web2fa
  docker compose --project-name web2fa --env-file current.env \
    -f compose.yaml -f compose.caddy.yaml up -d --no-build --remove-orphans
  docker inspect web2fa --format '{{json .NetworkSettings.Networks}}' | grep -F 'pomeva-net'
  curl --fail http://127.0.0.1:8081/
  ```

- Caddy 路由验证、重建或公网验收失败时，先保留 Caddy container ID、mounts、配置 hash、`docker compose ps` 和最近 10 分钟日志，再执行以下固定回滚：

  ```bash
  cd /home/ubuntu/pomeva
  caddy_backup='caddy/backups/config-before-web2fa.tar.gz'
  sudo sha256sum --check "${caddy_backup}.sha256"
  sudo tar --acls --xattrs --numeric-owner -xzf "$caddy_backup" \
    caddy/Caddyfile caddy/caddy.d
  sudo chown ubuntu:ubuntu caddy/Caddyfile caddy/caddy.d
  sudo find caddy/caddy.d -type f -exec chown ubuntu:ubuntu {} \;
  sudo chmod 0640 caddy/Caddyfile
  sudo chmod 0750 caddy/caddy.d
  sudo find caddy/caddy.d -type f -exec chmod 0640 {} \;

  caddy_image_ref="$(docker inspect server-sh-geet-caddy-1 --format '{{.Config.Image}}')"
  docker run --rm --pull never --network none --entrypoint caddy \
    --volume /home/ubuntu/pomeva/caddy/Caddyfile:/etc/caddy/Caddyfile:ro \
    --volume /home/ubuntu/pomeva/caddy/caddy.d:/etc/caddy/caddy.d:ro \
    "$caddy_image_ref" validate --config /etc/caddy/Caddyfile --adapter caddyfile
  docker compose config --quiet
  docker compose up -d --no-deps --force-recreate --wait --wait-timeout 60 caddy
  ```

  随后复核 Forgejo、Mihomo、TCP `80/443` 和无 UDP `443`。不要停止健康的 `web2fa` 容器，也不要删除 `caddy/data`、`caddy/config` 或备份。
- runner 故障不影响已运行的 `web2fa` 容器；停止 runner 只需在 `/opt/forgejo-runner` 执行 `sudo docker compose stop runner`，不得删除 DIND 数据卷或目标服务器镜像。

## 官方依据

- 本项目 Caddy 配置规则与实施前置方案：`/Users/ryanpenn/Workspace/Ai/cc-workspace/_servers/server-sh-geet/caddy-config-plan.md`
- Forgejo Actions 从 `.forgejo/workflows` 读取工作流，任务由独立 Forgejo Runner 执行：<https://forgejo.org/docs/latest/admin/actions/>
- Runner 通过 URL、UUID、TOKEN 建立连接：<https://forgejo.org/docs/latest/admin/actions/registration/>
- Docker 方式部署 runner 与 Docker-in-Docker 示例：<https://forgejo.org/docs/latest/admin/actions/installation/docker/>
- 在 Actions 中使用 Docker 需要额外 DIND 配置，且并发任务共享 DIND 存在安全风险：<https://forgejo.org/docs/latest/admin/actions/docker-access/>
- `on.push.paths`、`workflow_dispatch`、`runs-on` 和 `secrets` 上下文语法：<https://forgejo.org/docs/latest/user/actions/reference/>
- Caddy `handle_path` 会在执行内部处理器前剥离匹配的路径前缀：<https://caddyserver.com/docs/caddyfile/directives/handle_path>
- Caddy 配置应先 `adapt`/`validate`；本项目首次增加 `caddy.d` bind mount 或替换主 Caddyfile 时，按权威 runbook 受控重建 Caddy：<https://caddyserver.com/docs/command-line>

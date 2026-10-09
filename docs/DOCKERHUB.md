# 发布到 Docker Hub

项目镜像仅含应用、CA 和时区数据，不含 GeoLite2 数据库、名单快照或账户凭据。支持 `linux/amd64` 和 `linux/arm64`。仓库已提供发布 workflow，但不自动猜测 Docker Hub 用户名，也没有配置真实发布 token。

## 推荐：GitHub Actions

1. 在 [Docker Hub](https://hub.docker.com/) 自己的 namespace 下创建 `cch-geoip` repository。
2. 在 Docker 账户设置的 `Personal access tokens` 创建有 Read / Write 权限的 PAT；无需 Delete 权限。不要使用账户密码作为 CI secret。
3. 在 GitHub 仓库 `Settings > Secrets and variables > Actions` 配置：
   - Repository variable `DOCKERHUB_USERNAME`：Docker Hub 用户名，不一定与 GitHub 用户名相同。
   - Repository secret `DOCKERHUB_TOKEN`：上述 PAT。
4. 在 `Actions > Publish Docker Hub > Run workflow` 选择 `main`，填写版本，例如 `0.2.0`，不带 `v`。
5. workflow 先执行完整 CI（race、vet、进程和容器 smoke），全部通过后构建并推送双架构镜像：

```text
docker.io/<username>/cch-geoip:0.2.0
docker.io/<username>/cch-geoip:latest
```

只允许从 main 手动发布；普通代码 push 不会发布镜像。版本号应使用新值，避免覆盖已部署的固定版本。若 username 或 token 缺失，发布会失败，不会匿名发布。GeoLite2 / AbuseIPDB 凭据只用于部署，不要填成 Docker Hub secret。

发布后验证 manifest，检查包含两个架构：

```sh
docker buildx imagetools inspect docker.io/<username>/cch-geoip:0.2.0
```

GitHub CLI 也可触发：

```sh
gh workflow run dockerhub.yml --ref main -f version=0.2.0
gh run list --workflow dockerhub.yml
```

## 手动发布

先运行测试，再登录 Docker Hub。在 password 提示中输入 PAT，不把 token 写进命令行或仓库。

```sh
make test vet smoke
docker build -t cch-geoip:local .
make docker-smoke
docker login --username YOUR_DOCKERHUB_USERNAME
docker buildx create --name cch-geoip-publisher --driver docker-container --use
docker buildx build --platform linux/amd64,linux/arm64 \
  -t docker.io/YOUR_DOCKERHUB_USERNAME/cch-geoip:0.2.0 \
  -t docker.io/YOUR_DOCKERHUB_USERNAME/cch-geoip:latest --push .
docker buildx imagetools inspect docker.io/YOUR_DOCKERHUB_USERNAME/cch-geoip:0.2.0
```

`Dockerfile` 在 BUILDPLATFORM 上交叉编译 Go，无 TARGETPLATFORM 的 RUN 指令，因此此构建不需要 QEMU。若上述 builder 名已存在，用 `docker buildx use cch-geoip-publisher` 替代 create。生产部署优先固定版本或 digest，不依赖 latest。

## 运行已发布镜像

保留项目的 `docker-compose.yml` 和 `.env`，在 `.env` 设置实际发布地址：

```dotenv
CCH_GEOIP_IMAGE=docker.io/YOUR_DOCKERHUB_USERNAME/cch-geoip:0.2.0
```

```sh
docker compose pull
docker compose up -d --no-build
curl -f http://127.0.0.1:8080/readyz
```

首次仍需 updater 下载本地数据库。升级时只修改版本并重复 pull / up，不删除 volumes。没有发布镜像之前，请用 README 中的源码构建方式。

来源：[Docker PAT](https://docs.docker.com/security/access-tokens/personal-access-tokens/)、[GitHub Actions 多架构构建](https://docs.docker.com/build/ci/github-actions/multi-platform/)。

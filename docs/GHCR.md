# 发布到 GHCR

GHCR（GitHub Container Registry）托管镜像，GitHub Actions 负责构建；服务仍运行在自己的服务器上。镜像含应用、CA、时区和官方固定版本 ip2region xdb 及许可证，不含 GeoLite2 数据库、名单快照或账户凭据。支持 `linux/amd64` 和 `linux/arm64`。

## GitHub Actions 发布

1. 确保仓库允许 GitHub Actions 运行，组织策略没有禁止 workflow 写入 packages。
2. 在 `Actions > Publish GHCR > Run workflow` 选择 `main`，填写版本，例如 `0.6.0`，不带 `v`。
3. workflow 先执行完整 CI（race、vet、进程和容器 smoke）；全部通过后才构建、推送双架构镜像。

本仓库对应的发布地址为：

```text
ghcr.io/zhang0281/cch-geoip:0.6.0
ghcr.io/zhang0281/cch-geoip:latest
```

版本号仅是示例，不表示镜像已发布。只允许 main 手动发布，普通 push 不发布镜像；版本应使用新值，避免覆盖固定版本。Fork 会使用自己的仓库地址，workflow 将 namespace 和镜像名称转为小写。

发布 job 使用 GitHub 自动生成的 `GITHUB_TOKEN`，仅授予 `contents: read` 和 `packages: write`，无需新增发布 secret、用户名 variable 或个人 PAT。GeoLite2 / AbuseIPDB key 只用于部署，不提供给发布 workflow。镜像包含 source/revision/version labels，关联源码仓库。

也可从项目目录使用 GitHub CLI：

```sh
gh workflow run ghcr.yml --ref main -f version=0.6.0
gh run list --workflow ghcr.yml
```

首次发布会创建 package，无需预建镜像仓库。若同名 package 已由其他方式创建且未关联本仓库，GITHUB_TOKEN 可能没有写权限；需在 package 设置中关联仓库，并检查 `Manage Actions access`。

## 镜像可见性与拉取

**首次发布的 package 默认 Private，即使 Git 仓库是 Public。** 本项目不自动更改 package 可见性。

若要开放匿名拉取，发布完成后进入 GitHub 账号的 `Packages > cch-geoip > Package settings`，在 `Change visibility` 中选择 Public。公开前确认没有发布凭据或私有数据；源码公开不代表镜像自动公开。

Public 镜像不需要登录，可检查双架构 manifest：

```sh
docker buildx imagetools inspect ghcr.io/zhang0281/cch-geoip:0.6.0
docker pull ghcr.io/zhang0281/cch-geoip:0.6.0
```

若保持 Private，部署机需要使用有 package 读取权限的 GitHub 账号登录。创建带 `read:packages` scope 的 **PAT classic**，在密码提示中输入 PAT，不写进命令行或 Git：

```sh
docker login ghcr.io --username YOUR_GITHUB_USERNAME
```

这里的部署机 PAT 与 Actions 自动生成的 GITHUB_TOKEN 不同；后者不需要也不应复制到部署机。组织要求 SSO 时还需授权 PAT。

## 运行发布镜像

保留项目的 `docker-compose.yml` 和 `.env`，配置实际存在且可访问的版本：

```dotenv
CCH_GEOIP_IMAGE=ghcr.io/zhang0281/cch-geoip:0.6.0
```

```sh
docker compose pull
docker compose up -d --no-build
curl -f http://127.0.0.1:8080/readyz
```

首次仍需 updater 下载数据库；Geo 与 threat 的账户配置见 [ACCOUNTS.md](ACCOUNTS.md)。若更改宿主端口，修改 curl 地址。升级时修改版本再执行 pull / up，不删除 volumes；生产优先固定版本或 digest，而非 latest。未发布镜像时仍可用 README 的源码构建方式。

Dockerfile 在 BUILDPLATFORM 上交叉编译 Go，没有 TARGETPLATFORM 的 RUN 指令，此多架构构建不需要 QEMU。发布成功还应在目标服务器验证 manifest、pull 和运行；workflow 语法通过不能代替这些验收。

来源：[GHCR 权限与可见性](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[Actions 发布镜像](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)。

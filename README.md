# cch-geoip

为 Claude Code Hub (CCH) 提供兼容的 IP 归属地接口。日常查询直接读取本地 GeoLite2 City + ASN 数据库，不依赖外部 GeoIP API。官方 `geoipupdate` 容器定期更新数据库，本服务自动重新加载。

可选 threat 增强：Spamhaus DROP IPv4/IPv6 网段、Tor 官方出口名单，以及显式启用的 AbuseIPDB。本地 AbuseIPDB 黑名单优先，未命中才使用缓存或免费账户 CHECK。它们不阻断地理信息查询，也不执行封禁。

## 快速部署

需要 Docker Compose、MaxMind 免费账号的 Account ID 与 license key，以及该账号可下载的 GeoLite2 City / ASN 数据库。服务代码免费，数据使用仍遵循各提供方条款；仓库不包含真实数据库或 API 密钥。

先取得源码并进入项目。账户申请、API key 获取和额度见 [账户配置](docs/ACCOUNTS.md)。默认使用 `docker-compose.yml`，支持 AMD64 / ARM64。

```sh
git clone https://github.com/zhang0281/cch-geoip.git
cd cch-geoip
```

```sh
cp .env.example .env
chmod 600 .env
# 编辑 .env，填写 MAXMIND_ACCOUNT_ID、MAXMIND_LICENSE_KEY。
# 若启用 API 鉴权，还需填写 GEOIP_API_TOKEN。
docker compose up -d --build
docker compose logs --tail=50 geoipupdate geoip
curl -f http://127.0.0.1:8080/readyz
```

初次下载完成前 `/healthz` 为 200，`/readyz` 和查询接口为 503。数据库到位后默认一分钟内自动加载，也可 `docker compose kill -s HUP geoip` 立即检查。默认每 24 小时检查数据库更新；保留 `geoip-data` volume，升级时无需重下载已有数据库。不要用 `docker compose down -v` 升级。

```sh
curl 'http://127.0.0.1:8080/v1/ip2location/8.8.8.8?lang=zh-CN'
# 设置 GEOIP_API_TOKEN 后需带 Authorization: Bearer <token>。
```

Compose 仅绑定宿主 `127.0.0.1:8080`。若此端口已占用，在 `.env` 设置 `GEOIP_HOST_PORT`，并相应修改宿主 curl 地址；容器内仍为 8080。停止用 `docker compose down`，更新源码后用 `docker compose up -d --build`，保留 volumes。

## 其他运行方式

### 已发布 GHCR 镜像

先按 [GHCR 发布说明](docs/GHCR.md) 发布镜像。本仓库不预设已存在的镜像；发布后，在 `.env` 设置 `CCH_GEOIP_IMAGE=ghcr.io/zhang0281/cch-geoip:<version>`，再执行：

```sh
docker compose pull
docker compose up -d --no-build
```

Compose 的 build 定义用于源码模式；`--no-build` 确保镜像模式不会隐式重建。updater 与数据库、名单 volumes 在两种模式下相同。

Actions 使用自动生成的 GITHUB_TOKEN，无需额外发布凭据。首次 package 默认私有，公开 Git 仓库不自动公开镜像；需要匿名拉取时，在 package 设置中改为 Public。保持私有则部署机需先登录 GHCR，详见发布说明。

### 原生 binary

需要 Go 1.26+。先通过官方 geoipupdate 或账户下载页面取得 City / ASN 文件，再运行：

```sh
make build
GEOIP_CITY_DB=/absolute/path/GeoLite2-City.mmdb \
GEOIP_ASN_DB=/absolute/path/GeoLite2-ASN.mmdb \
THREAT_FEEDS_ENABLED=true THREAT_CACHE_DIR=/absolute/path/writable-cache \
./bin/cch-geoip
```

默认监听 `127.0.0.1:8080`；Ctrl+C / SIGTERM 正常退出，SIGHUP 检查数据库更新。binary 不自动读取 `.env`，需要由进程环境传入配置；开启 AbuseIPDB 时建议使用 `ABUSEIPDB_API_KEY_FILE`。Geo 数据库下载仍由外部 updater 管理，不在请求路径上在线补查。

## 接入 CCH

CCH 的 `IP_GEO_API_URL` 是 base URL，不能带 `/v1/ip2location` 后缀。将 CCH app 加入本服务网络，配置：

```dotenv
IP_GEO_API_URL=http://geoip:8080
IP_GEO_API_TOKEN=<与 GEOIP_API_TOKEN 相同的值>
IP_GEO_TIMEOUT_MS=1500
```

默认网络名称为 `<本项目 Compose project name>_geoip`。用 `docker network ls` 确认，再在 CCH Compose 中将它声明为 `external: true` 并加入 app 的 `networks`；保留 app 原有数据库/Redis 网络。修改环境变量后重新创建 CCH app。容器内的 `127.0.0.1` 指容器自身，不能用它连接另一个容器。

查询路径与返回字段依据 [CCH client](https://github.com/ding113/claude-code-hub/blob/main/src/lib/ip-geo/client.ts) 和 [types](https://github.com/ding113/claude-code-hub/blob/main/src/types/ip-geo.ts)。支持 IPv4、IPv6、中文和英文名称；指定语言无翻译时回退英文。没有记录的公网 IP 返回 `ZZ` 与 nullable ASN。私网、CGN、保留地址和 hostname 被拒绝，不做 DNS 或反向 DNS 请求。

## threat 与 privacy

Compose 默认启用本地名单，原生二进制默认关闭，需 `THREAT_FEEDS_ENABLED=true` 启用。名单每 24 小时更新，保存在独立的 `threat-data` volume；更新失败保留最后有效快照，超过 72 小时的数据不再参与匹配。名单下载不会发送被查询 IP。DROP 原始元数据和版权文本保留在缓存中；使用了 [The Spamhaus Project](https://www.spamhaus.org/blocklists/do-not-route-or-peer/) 的 DROP 数据。

- DROP 命中：`threat.is_threat=true`、`risk_level=critical`、本地策略分数 1，并附 `blocklists` 来源。此分数是策略映射，不是攻击概率。`listed_at` 为 null（CCH 的 RelativeTime runtime 支持，当前 TS 声明仍为 string），下载时间不冒充实际列入时间。
- Tor 出口命中：只设置 `privacy.is_tor` / `is_tor_exit` / `is_anonymous`，不会单凭 Tor 标成攻击者。
- AbuseIPDB：在 `.env` 设置 `ABUSEIPDB_ENABLED=true` 与 `ABUSEIPDB_API_KEY`。默认先下载 JSON 黑名单（最多 10,000 条）并本地匹配；有效名单命中时直接返回分数与 blocklist 来源，跳过 CHECK。未命中、名单不可用或过期时才读取 CHECK 缓存，必要时在线 CHECK。**开启 CHECK 后，未命中且未缓存的查询 IP 会发送给 AbuseIPDB。** 只读 BLACKLIST / CHECK，绝不自动 REPORT。
- 黑名单每 24 小时最多尝试下载一次，尝试时间与名单一起持久化，保留 volume 后失败及重启均不会立刻重复下载。更新失败保留旧名单；下载时间或 provider `generatedAt` 超过 `THREAT_MAX_AGE` 后停止匹配。缓存写入失败则跳过下载，避免持久限频失效；原生模式不设置缓存目录时无法跨重启限频。此限频只适用于单实例，不是多副本共享配额锁。
- BLACKLIST 与 CHECK 共用同一个 key，但上游分别计额。免费标准账户当前 BLACKLIST 5 次/天、10,000 条，CHECK 1,000 次/天。本项目不使用付费的自定义黑名单阈值，按实际返回分数本地判断。名单未命中不等于安全。
- AbuseIPDB 的 `abuseConfidenceScore / 100` 映射为分数，与 DROP 分数取最大值。阈值：0 为 none，(0,0.25) 为 low，[0.25,0.75) 为 medium，[0.75,0.9) 为 high，[0.9,1] 为 critical；分数 ≥0.75 才设置 `is_abuser` / `is_threat`。CHECK 使用 30 天记录；BLACKLIST 使用 provider 快照，不假称同一时间窗口。
- AbuseIPDB 默认成功缓存 6 小时，失败缓存 1 分钟，超时 500ms；日预算 900 次，收到 429 或凭据拒绝进入 cooldown。最多 16 个不同 IP 同时请求、4096 个缓存项，同 IP 并发合并。缓存与预算是进程内的，重启会重置；远端配额仍以 provider 响应为准。

`_meta.threat_sources` 标注各源的 `disabled` / `unavailable` / `stale` / `ready`、更新时间和命中状态；`abuseipdb_blacklist` 保留 `generatedAt` 对应的发布时间和命中的 `last_reported_at`。本地命中时 `abuseipdb_check.state=skipped`；`abuseipdb` 是实际选用结果。最后报告时间不是列入时间，blocklist 的 `listed_at` 仍为 null。VPN、开放代理、crawler、attacker、abuse 联系人等信息没有可靠数据源，保留 CCH 所需的兼容占位值。

仅本地名单模式：`ABUSEIPDB_ENABLED=true`、`ABUSEIPDB_BLACKLIST_ENABLED=true`、`ABUSEIPDB_CHECK_ENABLED=false`。仅 CHECK 模式：将 BLACKLIST 开关关闭。总开关 `ABUSEIPDB_ENABLED=false` 时，两个 provider 均关闭。

**CCH 当前 UI 不读取 `_meta`，会将 unknown 占位字段显示成 false / none。这是 UI 限制，不能将显示结果用于安全准入判断。** 本服务不自动修改 CCH，不自动封禁，不把 ASN organization 当作 ISP/公司所有者。

## 配置

| 环境变量 | 默认值 | 用途 |
|---|---|---|
| `GEOIP_LISTEN` | `127.0.0.1:8080`；Docker 为 `0.0.0.0:8080` | 监听地址 |
| `GEOIP_CITY_DB` | `/usr/share/GeoIP/GeoLite2-City.mmdb` | City 文件 |
| `GEOIP_ASN_DB` | `/usr/share/GeoIP/GeoLite2-ASN.mmdb` | ASN 文件 |
| `GEOIP_API_TOKEN` / `GEOIP_API_TOKEN_FILE` | 未设置 | Bearer 鉴权，二者互斥 |
| `GEOIP_RELOAD_INTERVAL` | `1m` | 文件替换检查间隔，或 SIGHUP |
| `GEOIP_MAX_DB_AGE` | `720h` | 根据 MMDB BuildEpoch 判断过期；0 显式禁用 |
| `THREAT_FEEDS_ENABLED` | `false`；Compose 为 `true` | 是否下载并匹配本地名单 |
| `THREAT_CACHE_DIR` | 空；Compose 为 `/var/lib/cch-geoip` | 空值时只保存在内存 |
| `THREAT_UPDATE_INTERVAL` | `24h` | 至少 24h |
| `THREAT_MAX_AGE` | `72h` | 过期名单停止参与匹配 |
| `ABUSEIPDB_ENABLED` | `false` | AbuseIPDB 总开关，明确 opt-in |
| `ABUSEIPDB_BLACKLIST_ENABLED` | `true` | 总开关开启后下载本地黑名单 |
| `ABUSEIPDB_CHECK_ENABLED` | `true` | 本地未命中时允许在线查询 |
| `ABUSEIPDB_API_KEY` / `ABUSEIPDB_API_KEY_FILE` | 未设置 | API key，二者互斥 |
| `ABUSEIPDB_TIMEOUT` | `500ms` | 最大 1s，留出 CCH 超时余量 |
| `ABUSEIPDB_CACHE_TTL` | `6h` | 成功缓存有效期 |
| `ABUSEIPDB_DAILY_BUDGET` | `900` | UTC 日、本进程预算，配合账号限额调整 |

文件形式的密钥可通过 Docker secret 挂载后设置 `_FILE` 环境变量。不要将真实 `.env`、secret 文件或 MMDB 提交到仓库。外部暴露服务须自行配置 TLS、鉴权和网关 rate limit；默认部署用于 Docker 内网或 localhost。

Compose 额外支持 `CCH_GEOIP_IMAGE`（默认 `cch-geoip:local`）与 `GEOIP_HOST_PORT`（默认 8080）。账户与额度参阅 [ACCOUNTS.md](docs/ACCOUNTS.md)，发布步骤参阅 [GHCR.md](docs/GHCR.md)。

## 开发与验证

需要 Go 1.26+，进程级 smoke test 另需 Python 3。

```sh
make test        # race detector、覆盖率；动态生成测试 MMDB，无需账号或网络
make vet
make smoke       # 实际启动进程，验证查询/鉴权/SIGHUP/损坏更新回退/正常退出
make build
make docker-smoke # 先构建 cch-geoip:local 镜像，再验证 non-root/cache/CA
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/cch-geoip-arm64 ./cmd/cch-geoip
```

Dockerfile 用 BuildKit 的 TARGETARCH 支持 AMD64 / ARM64。`docker buildx build --platform linux/amd64,linux/arm64 .` 可构建两种架构。`GO_IMAGE` build arg 可替换受限环境中的 builder，`GOPROXY` build arg 可设置依赖代理；最终镜像为 non-root scratch，包含 CA certificates 与嵌入式时区数据。运行镜像只读，数据库只读挂载，唯一可写路径是名单缓存 volume。

测试 fixture 均为合成记录，不能用来判断真实 IP 的归属地或信誉。GeoLite2 城市精度与覆盖有局限，未提供的坐标在 `_meta.coordinates_available` 标为 false。真实 MaxMind 下载、真实 AbuseIPDB 查询及 CCH UI 部署验收需另外验证。

参阅 [DESIGN.md](DESIGN.md)、[验证记录](docs/VALIDATION.md) 和 [数据来源说明](docs/SOURCES.md)。

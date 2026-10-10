# 账户、凭据与免费额度

核对日期：2026-10-10。真实凭据只填写在部署机器的 `.env`，权限设为 `600`，不要发到聊天、Issue 或 Git。`.env` 已由 Git 和 Docker build context 排除。无需将数据提供方凭据填写到 CCH。

## MaxMind GeoLite2

1. 注册 [GeoLite2 免费账户](https://www.maxmind.com/en/geolite2/signup)，完成邮件验证和提供方要求的账户步骤。
2. 登录账户，在账户页面取得数字 Account ID；进入 `Manage License Keys`，选择生成 license key。Key 仅显示一次，立即存入自己的密码管理器。
3. 在账户下载列表确认可下载 `GeoLite2-City` 和 `GeoLite2-ASN`。
4. 在 `.env` 填写：

```dotenv
MAXMIND_ACCOUNT_ID=your_numeric_account_id
MAXMIND_LICENSE_KEY=your_license_key
```

Compose 将这两项分别传给官方 updater 的 `GEOIPUPDATE_ACCOUNT_ID`、`GEOIPUPDATE_LICENSE_KEY`。查询服务只读取 MMDB，既不持有 MaxMind 下载 key，也不调用 MaxMind 在线 Geo 服务。

免费账户当前每 24 小时最多 30 次数据库下载；检查更新不计入下载次数。本项目每天检查 City 与 ASN；有更新才下载完整数据库。本地查询不计 API 次数，但仍应遵守数据许可，不把 MMDB 打进公开镜像或提交 Git。

来源：[生成 key](https://support.maxmind.com/knowledge-base/articles/generate-a-maxmind-license-key)、[下载与更新](https://support.maxmind.com/knowledge-base/articles/download-and-update-maxmind-databases)。

## AbuseIPDB

1. 注册 [AbuseIPDB 免费账户](https://www.abuseipdb.com/register)，登录后从账户控制台获取 API Key。
2. 在 `.env` 显式开启；下载黑名单和 CHECK 共用同一个 key：

```dotenv
ABUSEIPDB_ENABLED=true
ABUSEIPDB_API_KEY=your_api_key
ABUSEIPDB_BLACKLIST_ENABLED=true
ABUSEIPDB_CHECK_ENABLED=true
ABUSEIPDB_DAILY_BUDGET=900
```

免费标准账户当前 BLACKLIST 每天 5 次、每次最多 10,000 条；CHECK 每天 1,000 次，分别计额。项目固定下载最多 10,000 条的 JSON 黑名单，每 24 小时最多尝试一次；保留 volume 时尝试记录跨重启生效，下载失败不会立即反复重试。没有使用付费的阈值、国家等定制参数。

有效本地黑名单命中即返回信誉结果，不调用 CHECK。未命中或名单过期时，优先读取 CHECK 缓存，必要时才发送被查询 IP 到 AbuseIPDB；默认缓存 6 小时、本进程 UTC 日预算 900 次。多个实例或其他程序使用同一账户仍共同消耗上游额度；CHECK 缓存和预算不跨重启持久化。

只想下载名单、不发送单个查询 IP 时，设置 `ABUSEIPDB_CHECK_ENABLED=false`。完全不使用 AbuseIPDB 时保持 `ABUSEIPDB_ENABLED=false`，无需 API Key。Geo、DROP 和 Tor 不受影响。

名单未命中不代表安全；分数是 abuse confidence，不是攻击概率。更新失败保留上次有效名单，数据超过 72 小时停止参与匹配；在线接口失败或额度耗尽会在 `_meta` 标记未知。

来源：[BLACKLIST](https://docs.abuseipdb.com/#blacklist-endpoint)、[CHECK](https://docs.abuseipdb.com/#check-endpoint)、[配额](https://docs.abuseipdb.com/#api-daily-rate-limits)。

## 国外免 Key 在线 Geo

0.6.0 国内使用 IP.SB，失败立即回退 ip2region/GeoLite2，不调用 ipwho.is/GeoJS；国外保持 IP.SB → ipwho.is → GeoJS。三者均不需要账户或API Key。无需注册ipapi.is；旧 `IPAPI_IS_API_KEY` / `_FILE` / `IPAPI_IS_DAILY_BUDGET` 已不使用，无需读取或迁移它们。MaxMind下载凭据与可选AbuseIPDB凭据仍按上文配置。

```dotenv
ONLINE_GEO_ENABLED=true
ONLINE_GEO_CACHE_TTL=24h
ONLINE_GEO_TIMEOUT=2s
IPSB_DAILY_BUDGET=900
IPWHO_IS_DAILY_BUDGET=900
GEOJS_DAILY_BUDGET=900
```

- [IP.SB](https://ip.sb/api/)：免费指定IPv4/IPv6查询，无Key；当前100次/分钟且最多5次/秒/出口IP。本项目使用专用User-Agent、90次/分钟和4次/秒限频，另有900次/24h本地预算。免费说明面向个人项目和轻量集成；商业产品需按提供方说明咨询。
- [ipwho.is](https://ipwhois.io/documentation)：免费HTTPS、1000次/日/出口IP，无Key，现有备用保留。
- [GeoJS](https://www.geojs.io/docs/v1/endpoints/geo/)：免费指定IP查询，返回公里半径；没有公布固定免费额度，受[合理使用与限流](https://www.geojs.io/tos/)约束。本地默认900次/24h预算不等于官方保证。

缓存与预算自动落盘；0.4.0旧状态升级保留ipwho.is消耗，删除已退役ipapi数据。不删缓存目录来重置额度，多实例/同出口其他程序仍应协调限额。在线provider查询仅用于自己的CCH日志，服务端口保持内网；无需将其包装为公共IP查询产品。

修改配置后只重建查询服务、保留volume：

```sh
docker compose up -d --no-deps --no-build geoip
```

CCH默认建议3500ms；现有1500ms可搭配900ms在线总时限。完全本地查询设置 `ONLINE_GEO_ENABLED=false`。

## 本服务鉴权与 Docker secrets

`GEOIP_API_TOKEN` 是自己生成的随机密钥，不是上述 vendor key；CCH 的 `IP_GEO_API_TOKEN` 填相同值。可在本机用 `openssl rand -hex 32` 生成，再填入 `.env`，不要将输出贴到公开渠道。

```dotenv
GEOIP_API_TOKEN=your_own_random_token
```

默认 Compose 使用环境变量传递凭据；有 Docker 管理权限的人可以读取容器环境。要求文件形式时需修改 Compose 挂载 secrets，并用 `GEOIP_API_TOKEN_FILE`、`ABUSEIPDB_API_KEY_FILE` 替换对应直接值。官方 updater 支持 `GEOIPUPDATE_ACCOUNT_ID_FILE`、`GEOIPUPDATE_LICENSE_KEY_FILE`。只在 `.env` 设置 `_FILE` 不会自动挂载文件；同一凭据直接值与文件值互斥。

修改 `.env` 后执行 `docker compose up -d --build` 重新创建受影响服务；保留数据库及名单 volumes。不要把 `docker compose config` 的完整输出公开，它可能展开真实密钥；只验证语法时用 `docker compose config --quiet`。

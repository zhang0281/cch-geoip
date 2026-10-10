# 数据与接口来源

- [ip2region v3.18.0](https://github.com/lionsoul2014/ip2region/releases/tag/v3.18.0)、[数据与更新说明](https://github.com/lionsoul2014/ip2region/blob/v3.18.0/README_zh.md)、[许可证](https://github.com/lionsoul2014/ip2region/blob/v3.18.0/LICENSE.md)：IPv4/IPv6 xdb 用于国内省市和 ISP，免费数据不定期更新，不等于已保证的当前实际位置。镜像附完整许可证；SHA256 v4 `8e31bbdccb5bf21028af10592d4312ec975da0bffa108c0c5d862a12190f9ad3`，v6 `939f6b46bd2b8bec3cf7c5ceb8ba782266ae9b1f35b5ba7916700dec0b7506ed`。

- CCH 路径/鉴权/校验器：[client.ts](https://github.com/ding113/claude-code-hub/blob/main/src/lib/ip-geo/client.ts)。返回字段：[ip-geo.ts](https://github.com/ding113/claude-code-hub/blob/main/src/types/ip-geo.ts)。2026-10-09 核对；可接受字段不等于未来所有 CCH 版本的保证。
- MaxMind [GeoLite 说明](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data/) 与 [数据更新](https://support.maxmind.com/knowledge-base/articles/download-and-update-maxmind-databases)。City 当前周二/周五，ASN 工作日；本地查询无 web service 次数限制。账号下载与数据使用须遵循提供方许可。
- 官方 [geoipupdate Docker](https://github.com/maxmind/geoipupdate/blob/main/doc/docker.md)。Compose 固定 v8.0.0，每 24 小时检查 City / ASN。注册 key 不应出现在 Git、CLI 参数或日志中。
- [Spamhaus DROP](https://www.spamhaus.org/blocklists/do-not-route-or-peer/) 与 [条款](https://www.spamhaus.org/drop/terms/)。匹配源为 drop_v4.json / drop_v6.json，保留 ©、日期和条款文本；产品标注 The Spamhaus Project。每天下载一次。名单仅覆盖特定恶意网络，不是完整 IP 信誉库。
- [Tor 官方出口列表](https://check.torproject.org/torbulkexitlist)。仅证明在下载时的出口名单中出现，不证明攻击或完整 Tor 网络覆盖。
- [AbuseIPDB CHECK API](https://docs.abuseipdb.com/#check-endpoint) 与 [配额](https://docs.abuseipdb.com/#api-daily-rate-limits)。标准账户当前 CHECK 1000/日；项目预算默认 900。置信度/报告有时间范围，不等于未来行为概率。只读接口、maxAgeInDays=30，不请求 verbose，不调用 REPORT。
- [AbuseIPDB BLACKLIST](https://docs.abuseipdb.com/#blacklist-endpoint)：标准账户当前 5 次/日，最多 10,000 条。使用 JSON 保留 generatedAt、abuseConfidenceScore、lastReportedAt；免费账户不依赖付费的自定义 confidenceMinimum。每 24 小时最多一次下载尝试，本地未命中不能证明安全。账户获取见 [ACCOUNTS.md](ACCOUNTS.md)。
- [GeoIP Anonymous IP](https://dev.maxmind.com/geoip/docs/databases/anonymous-ip/) 是可选的付费 VPN/proxy 数据方向；本项目当前不实现，GeoLite2 不提供这些字段。
- [CrowdSec CTI](https://docs.crowdsec.net/u/cti_api/intro/) 能提供行为上下文，但本轮不采用，避免将有限配额变成每个 GeoIP 请求的依赖。

所有测试 MMDB 均为项目生成的合成数据；仓库不再分发真实 GeoLite2 数据。第三方库的许可证以其各自 LICENSE 为准。

- 国外在线 Geo（2026-10-10核对）：[ipapi.is API](https://ipapi.is/developers.html)、[条款](https://ipapi.is/terms.html)，注册免费账户1000次/日、完整响应、缓存最多30天、内部用途；匿名接口格式和额度不作为本项目依赖。[ipwho.is](https://ipwhois.io/documentation) 免费HTTPS、IPv4/IPv6、1000次/日/出口IP，省市坐标与时区；不采用付费security数据。各provider可调整免费政策，本项目预算/退避不是上游额度保证。

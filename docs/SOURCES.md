# 数据与接口来源

- CCH 路径/鉴权/校验器：[client.ts](https://github.com/ding113/claude-code-hub/blob/main/src/lib/ip-geo/client.ts)。返回字段：[ip-geo.ts](https://github.com/ding113/claude-code-hub/blob/main/src/types/ip-geo.ts)。2026-10-09 核对；可接受字段不等于未来所有 CCH 版本的保证。
- MaxMind [GeoLite 说明](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data/) 与 [数据更新](https://support.maxmind.com/knowledge-base/articles/download-and-update-maxmind-databases)。City 当前周二/周五，ASN 工作日；本地查询无 web service 次数限制。账号下载与数据使用须遵循提供方许可。
- 官方 [geoipupdate Docker](https://github.com/maxmind/geoipupdate/blob/main/doc/docker.md)。Compose 固定 v8.0.0，每 24 小时检查 City / ASN。注册 key 不应出现在 Git、CLI 参数或日志中。
- [Spamhaus DROP](https://www.spamhaus.org/blocklists/do-not-route-or-peer/) 与 [条款](https://www.spamhaus.org/drop/terms/)。匹配源为 drop_v4.json / drop_v6.json，保留 ©、日期和条款文本；产品标注 The Spamhaus Project。每天下载一次。名单仅覆盖特定恶意网络，不是完整 IP 信誉库。
- [Tor 官方出口列表](https://check.torproject.org/torbulkexitlist)。仅证明在下载时的出口名单中出现，不证明攻击或完整 Tor 网络覆盖。
- [AbuseIPDB CHECK API](https://docs.abuseipdb.com/#check-endpoint) 与 [配额](https://docs.abuseipdb.com/#api-daily-rate-limits)。标准账户当前 CHECK 1000/日；项目预算默认 900。置信度/报告有时间范围，不等于未来行为概率。只读接口、maxAgeInDays=30，不请求 verbose，不调用 REPORT。
- [GeoIP Anonymous IP](https://dev.maxmind.com/geoip/docs/databases/anonymous-ip/) 是可选的付费 VPN/proxy 数据方向；本项目当前不实现，GeoLite2 不提供这些字段。
- [CrowdSec CTI](https://docs.crowdsec.net/u/cti_api/intro/) 能提供行为上下文，但本轮不采用，避免将有限配额变成每个 GeoIP 请求的依赖。

所有测试 MMDB 均为项目生成的合成数据；仓库不再分发真实 GeoLite2 数据。第三方库的许可证以其各自 LICENSE 为准。

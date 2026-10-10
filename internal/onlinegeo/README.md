# onlinegeo

提供免Key在线地域查询，供internal/api使用；国内只使用IP.SB，ip2region/GeoLite2托底由API管理。仅依赖标准库。

调用New(Options)恢复缓存与限额，Lookup(ctx,ip,now)用于国外三渠道，LookupDomestic(ctx,ip,now)只使用IP.SB及CN缓存；Location=nil表示离线回退。国内绝不消费其他provider缓存/flight；inflight按IP和策略区分，共用额度与并发上限。不需要账户或Key。固定HTTPS GET及专用User-Agent，限时/禁止redirect/限长并校验响应。GeoJSAccuracyRadiusKM只使用真实公里半径；未知保持nil。

每个provider本地滚动24h预算默认900，IP.SB另限90/min、4/sec；失败计额且先落盘后请求。同IP合并、16不同IP并发、4096缓存项。缓存默认24h最多7天，Maintain(now)每分钟由main触发清理。New拒绝坏状态，不静默重置限额。v1迁移保留ipwho配额/cache，删除ipapi数据。

单进程可写CacheDir，保留目录才能跨重启限频；多个副本或同出口其他程序不共享锁。配置见[README](../../README.md#在线-geo-与离线回退)，免费条件见[ACCOUNTS](../../docs/ACCOUNTS.md#国外免-key-在线-geo)，设计见[DESIGN](DESIGN.md)。验证go test -race ./internal/onlinegeo。

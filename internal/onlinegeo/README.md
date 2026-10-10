# onlinegeo

为国外 IP 提供免费在线地域补查，供 `internal/api` 使用；国内门控和 GeoLite2 回退由 API 层管理。仅依赖 Go 标准库。

调用 `New(Options)` 初始化并恢复持久缓存，再用 `Lookup(ctx, ip, now)` 获取 `Result`。`Location=nil` 表示应使用离线结果；`Providers` 保留已尝试/跳过原因。ipapi.is Key 缺失时跳过，使用无需Key的ipwho.is。

运行配置与示例见 [根README](../../README.md#国外在线-geo-与离线回退)，账户见 [ACCOUNTS](../../docs/ACCOUNTS.md#ipapiis-免费账户与-ipwhois)。

`Options.CacheDir` 必须可写，服务先保存额度才发请求；状态损坏拒绝初始化。单实例保留目录才能跨重启限频。缓存最多4096项、默认24h；provider默认滚动24h预算900次，失败计额；同IP并发合并，总时限2s，各provider最多一半。没有精度半径时不从可信度等级生成半径。

验证：`go test -race ./internal/onlinegeo`。设计见 [DESIGN](DESIGN.md)。

在线服务运行时独立每分钟清理过期缓存，即使没有新请求也会落盘删除；停服时不执行清理。维护原子状态时保留未过期额度记录。

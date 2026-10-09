# cch-geoip 设计

## 目标与边界

用本地 City / ASN 数据替代 CCH 默认外部 GeoIP 服务，并提供可选、可追溯的 threat 增强。假设是单机 Docker 内网服务、读多写少、无需用户数据库、响应应落在 CCH 默认 1500ms 超时内。主要优化部署简单与可维护性，不引入 Redis、消息队列或 Web UI。

不修改 CCH；不提供安全准入、自动封禁或真实身份判断；不声称 GeoLite2 能识别 VPN 或攻击者。

## 架构与不变量

```text
MaxMind HTTPS → 官方 geoipupdate → City / ASN MMDB volume [外部数据→本地文件]
                                            ↓ 只读
CCH → CCH-compatible Go HTTP API → MMDB readers [调用方→查询接口]
                              ↘ 本地 Spamhaus DROP / Tor snapshot
                              ↘ 可选 AbuseIPDB CHECK [被查询 IP→第三方，显式 opt-in]
Spamhaus / Tor HTTPS → 每日更新 → 持久缓存 → 原子替换内存 snapshot
```

核心代码：`internal/geoip` 管数据库生命周期；`internal/api` 管 CCH JSON 与输入边界；`internal/threat` 管信誉来源、状态和降级；`internal/config` 管环境变量校验；`cmd/cch-geoip` 管进程生命周期。`internal/testdb` 和 `tools/fixtures` 仅生成合成测试数据，生产 binary 不引用它们。

- 有效查询需要 City、ASN 都已加载且未超过 BuildEpoch 年龄上限。
- MMDB readers 在 lookup 持 RLock，替换/close 持写锁，避免 mmap use-after-close。
- 更新时先打开并验证两个候选 reader，检查数据库类型及文件标识未变化，再整体切换；失败保留旧 reader。由于官方 updater 独立更新两个 edition，它们不保证同一发布时间，本服务也不假称同版本事务。
- updater 须通过原子 rename 发布新文件，不可就地 truncate 正在 mmap 的文件。官方 updater 的文件替换机制与本服务相配。
- health 表示进程可响应；ready 表示 GeoIP 可服务。更新失败但旧数据仍未过期时 ready 保持 true，并暴露 `reload_healthy=false`；旧数据过期则 ready/query 为 503。
- threat I/O 不在数据库锁内。任何 threat 故障不改变已有 GeoIP 结果。

## 方案取舍

直接使用现成 GeoIP REST 服务需要再加协议转换进程；本方案用 Go 标准 HTTP + MMDB reader 直接输出 CCH 协议，少一层网络与生命周期。Python 可更快原型，但需要解释器运行时；Go 的静态 binary、内置时区与 AMD64/ARM64 cross build 更适合该 Docker sidecar。

数据库更新使用 MaxMind 官方工具，而非重写下载、解压、授权与调度。应用只读数据库；单独 writer 容器拥有下载凭据。Go reader 固定到已验证的 v1 API，升级依赖必须重新通过真实 MMDB 和 race 测试。

CCH 要求 `threat.score` 为数字、privacy/threat flags 为布尔。为了不破坏现有 dialog，未支持字段使用占位值，并在 `_meta` 记录数据源与限制。CCH UI 忽略 `_meta`，因此无法正确展示 unknown；这是已知接口表达限制，不通过伪造数据补全。

## Threat 语义

首版加载 DROP v4/v6 与 Tor 出口；ASN-DROP、crawler 确认、VPN 库留待明确数据源后添加。DROP 匹配代表上游认定的恶意网络，使用本地 critical / 1 策略值；它不是攻击概率，也不证明每次请求恶意。Tor 属 privacy 信号，不增加 threat 分数。

AbuseIPDB opt-in 使用 30 天内的 abuse confidence；不把 reports 数量直接当 attacker 证据。与 DROP 取最大值的聚合政策公开在 README / `_meta`，来源状态分别保留。只有分数达到 0.75 才设置 is_abuser / is_threat；阈值是本项目政策，不是提供方官方分类。

名单在背景下载，体积上限 8 MiB，必须通过完整解析；DROP 保存原始 metadata/copyright，并检查发布时间。重新下载失败保留旧快照，超过 max age 则退出匹配。每天最多一次成功更新检查；持久缓存跨重启抑制重复成功下载。进程失败、缓存丢失或下载失败后的重启仍可能重试，不构成跨实例的全球调度保证。

AbuseIPDB 使用固定 HTTPS endpoint、禁止 redirect（防 API key 被转发），限制响应 64 KiB。成功/失败缓存、同 IP 并发合并、16 个不同 IP 的并发上限、4096 项缓存限制、UTC 日预算及 429 cooldown 共同控制配额。预算/缓存不持久化，重启及多实例无法共用预算；部署者须保留远端配额余量。不开启后台自动 REPORT，不持久化被查询 IP。

## 安全与运维

威胁模型为恶意调用者、错误/损坏的外部数据、故障 provider。输入只允许 IP literal 和语言 allowlist，不接受 URL/host，不做反向 DNS。API token 用 SHA256 后 constant-time compare；可通过 secret 文件输入。查询 IP 和 keys 不写业务日志。

默认 localhost/内网，Compose 不公开宿主所有网卡；容器 non-root、只读 rootfs、cap_drop ALL、no-new-privileges。GeoIP volume 只读；缓存仅该服务 writable；CA 与 tzdata 随应用镜像提供。HTTP header/read/write/idle 都设超时与 header 大小限制。外网服务的 TLS 和 rate limit 属网关职责，本项目不默认开放公共接口。

SSRF 面被固定 provider URL 和 IP literal 验证收窄。AbuseIPDB 只向固定 endpoint 发送 IP，不跟随重定向；免费 feed 的 HTTPS 重定向由提供方控制且无凭据。GeoLite2 下载 key 仅由 updater 使用。环境变量不输出完整 Config，避免日志泄露。仓库忽略 .env / secrets / MMDB / 缓存 / binary。

## 验证与已知限制

真实 MMDB 读写由合成 fixture 验证，覆盖 IPv4/IPv6、语言 fallback、未知 IP、损坏更新/恢复、过期以及并发切换。HTTP 检查 CCH validator 关键字段与 UI 数组/数值字段，覆盖鉴权、非公网 IP、无数据库。threat 测试含名单格式、缓存恢复/过期、Tor 不等于攻击、Abuse 429/401/超时/错 IP/非法分数/redirect、预算及并发合并。进程 smoke 验证 signal 生命周期；Docker runtime smoke 验证 UID、volume 权限、health 与 HTTPS CA。

未测 gate 必须列于 docs/VALIDATION.md：官方账号下载、真实 AbuseIPDB、Oracle 部署与真实 CCH UI，不能用 mock 成功替代它们。GeoLite2 City + ASN 缺少人口、币种、公司、联系人和风险画像，无法与商业 IP intelligence 数据齐全等价。

## 变更历史

2026-10-09：首版独立服务、官方 updater Compose、CCH-compatible API、local DROP/Tor 与 opt-in AbuseIPDB，加入 race / 进程 smoke / cross build / CI。

质量复核接受项：`scripts/smoke.py` 与 `scripts/docker-smoke.py` 的 main 超过通用 50 行 heuristic；它是单次进程验收编排，统一 try/finally 保证子进程与临时目录清理，业务逻辑在 Go 包中。JSON/schema 常量与合成 fixture 的长行属于固定数据，不额外抽象。

CCH 日期边界：blocklists 的真实列入时间未知时 `listed_at` 返回 null，避免 RelativeTime 组件对空字符串执行 toISOString 抛异常。其 runtime 接受 null，但 CCH 当前 TypeScript 声明仅 string；这是有意的 nullable 扩展，未来 CCH 若加强 schema 需重新验证。没有拿下载时间伪装成列入时间。

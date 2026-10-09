# 验证记录

时间：2026-10-09。源码、配置与测试在 `/home/zhang/work/projects/cch-geoip`。未部署到 Oracle，也未修改既有 CCH。

## BLACKLIST 与发布配置增量验证

2026-10-09，Go 1.26.9：

- `make test vet smoke docker-smoke` 全部通过；race tests 中 threat 覆盖率 87.9%、config 96.7%，API / GeoIP 覆盖率保持 94.1% / 84.9%。
- BLACKLIST mock 验证 JSON/IP/分数/时间戳/重复记录校验、IPv6、合法空列表、并发合并下载、命中跳过 CHECK、未命中读缓存/在线补漏、过期排除、401/403/429/503/坏 JSON 保留有效旧快照。
- 下载尝试在网络前落盘，测试失败后重启不重复请求、首次失败无数据快照的限频恢复、缓存写入失败不下载、redirect 不访问目标。
- 新增 `scripts/blacklist-smoke.py`，启动真实 binary，验证 config→Restore→Service→CCH JSON 路径；新鲜名单输出 score/flags/blocklist/last_reported_at，过期不评分，CHECK 关闭后未命中为 unknown。使用合成名单和凭据，已有 Attempt 阻止在线下载，没有请求真实 AbuseIPDB。
- Dockerfile 用已校验的本地 Go builder 构建成功；scratch/non-root/read-only-rootfs 中现有 Geo 查询与新增 BLACKLIST 缓存写入/恢复通过。宿主 Docker Hub builder 直连约束同首版，默认远端 builder 由 GitHub CI 验证。
- ARM64 静态交叉编译通过，未新增 ARM64 真机验收。
- `docker-compose.yml` 默认、local-only、镜像模式用合成凭据展开检查；`.env.example` 与服务配置对应，无真实密钥落盘。
- `actionlint` 校验 CI reusable workflow 与 Docker Hub 手动发布 workflow 通过。Docker Hub PAT/username 未配置，本轮未发布 Docker Hub 镜像；发布后 manifest / pull / runtime 仍需验收。
- `govulncheck`：No vulnerabilities found；模块、变更、静态安全和质量扫描通过。通用扫描仍有既有 smoke 编排函数长度警告，不替代执行测试。

运行方式见 README；账户获取见 [ACCOUNTS.md](ACCOUNTS.md)，发布见 [DOCKERHUB.md](DOCKERHUB.md)。真实账号下载、真实 AbuseIPDB 到达性与 Oracle/CCH UI gate 仍未执行。

## 首版已执行

- Go 1.26.9（官方 archive SHA256 校验后使用）；`go test -race -cover ./...` 全部通过。
- 核心包覆盖率：API 94.1%、config 96.5%、GeoIP 84.9%、threat 83.0%。主进程与 fixture CLI 由外部 smoke 验证，未计入 Go 单测覆盖率；整体覆盖率约 74%。
- `go vet ./...`、gofmt 检查通过。
- `make smoke`：真实进程的 HTTP、Bearer 鉴权、中文名称、MMDB 读取、SIGHUP 热更新、损坏更新保留旧 reader、healthcheck、SIGTERM 均通过。
- Dockerfile 完整多阶段构建及 `make docker-smoke` 通过；实际 scratch/non-root UID 65532/read-only rootfs 查询，CA pool/内置 tzdata、命名 volume 写权限/缓存恢复、healthcheck 与退出均通过。
- 本机 Docker Hub Go 镜像直连受阻，构建时仅替换 `GO_IMAGE` 为使用 SHA256 已校验官方 Go archive 的本地 builder，并设置可用的 GOPROXY。生产 Dockerfile 默认 builder 的远端拉取由 GitHub CI 验证；最终镜像结构、entrypoint、权限与打包路径相同。
- AMD64 与 ARM64 静态 ELF cross build 通过；ARM64 未在真实 ARM 硬件运行。官方 updater v8.0.0 manifest 已确认 linux/amd64、linux/arm64 等架构。
- `docker compose config --quiet` 使用合成占位凭据通过，未尝试假凭据下载。
- Spamhaus DROP 真实已下载数据解析通过：IPv4 1672 个前缀、IPv6 91 个前缀。Tor 官方真实已下载出口名单解析通过：1212 个 IP。仅验证格式与本地解析，不验证每个 IP 的信誉真实性；这些外部文件不提交 Git。
- AbuseIPDB 使用模拟 HTTP server 验证成功/错误/超时/429/401/预算/缓存/并发合并/错误 IP/非法 score/redirect 不转发 key，未请求真实 API。
- `govulncheck` 最终报告 No vulnerabilities found。曾发现 x/sys Windows 模块级问题（生产 Linux 路径未调用），仍已升级到 v0.44.0 修复版本后重扫。
- 文档完整性、变更分析、静态安全模式扫描通过。质量扫描通过，smoke 编排函数长度警告已在 DESIGN 说明接受理由；这些 heuristic 扫描不替代上述执行测试。
- CCH upstream validator 必需字段和 dialog 直接读取的数组/数值结构已测试。发现未知 `listed_at` 的空字符串会触发 CCH RelativeTime 日期异常，改用 null 并补回归测试；runtime 支持，但 CCH TS 声明为 string，作为明确记录的 nullable 扩展。

## 尚需真实环境验收

1. 前辈的 MaxMind 账号是否有权下载 City / ASN、实际下载更新及数据库权限；无人为生成/获取真实凭据。
2. 自己的 AbuseIPDB key、Oracle 的到达性及账号配额；默认禁用第三方查询。
3. Oracle 上容器部署、CCH 网络/env 挂载与真实 UI；CCH 当前忽略 `_meta`，unknown 仍可能被 UI 显示成 none/false。
4. ARM64 runtime 与远端 GitHub CI 状态；cross build 不等于硬件验收。

执行输出在本地 ignored `artifacts/`；实际数据库、缓存、二进制、密钥均未纳入源码。推送与 CI 结果在完成后由 Git/Actions 记录核验。

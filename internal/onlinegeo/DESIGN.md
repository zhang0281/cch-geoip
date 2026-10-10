# onlinegeo设计

2026-10-10：用户无法注册免费ipapi账户，改为三个免KeyAPI顺序失败回退；对比纯离线的国外覆盖不足，以及并发扇出消耗额度且冲突难解释，选择缓存→IP.SB→ipwho.is→GeoJS。固定GET/专用User-Agent/HTTPS/禁止redirect；响应验证限制恶意、错误或不完整外部数据。所有Geo服务均不持有Key。

状态v2三个provider配额，v1严格验证保守迁移：ipwho尝试/cooldown及其有效缓存保留，退役ipapi数据删除。每次网络前持久预留额度，失败也计额；坏状态不能清零，主进程禁用在线保留offline。IP.SB90/min及4/sec限频不sleep而转备用。IPwho上游每日1000、另两个上游政策不同，各900/24h仅项目预算。

网络不持锁，原子rename/fsync状态写持锁；同IP合并，有限并发和缓存大小，默认24h缓存、最长7天，每分钟空闲维护。磁盘写延迟可能拖慢缓存调用；单实例，无跨进程锁，同出口其他客户端耗额不在本地计数内。coords及country/timezone由一provider提供，GeoJSaccuracy按公里保留；未知半径不合成。

go tests模拟3provider顺序、限频重启、旧状态迁移、错误响应、cancel/concurrency；Docker验证非root/writablevolume；Oracle连通性和准确率分别验收。[根设计](../../DESIGN.md#国外免-key-在线优先2026-10-10050)。

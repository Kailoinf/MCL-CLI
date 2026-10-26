# 设计决策

每一条都是：决定、理由，以及代价。写出来就是为了被质疑。

## 用 TOML 而不是数据库——以及五个文件而不是一个

需求里写的是「配置放在 `.mcl-cli/mcli.db`，带一个 `config` 表」。一个叫 `.db` 却不是数据库的文件，对后来用 `sqlite3` 打开它的人是个陷阱；而真正的 SQLite 意味着一个沉重的无 cgo 驱动（`modernc.org/sqlite`）或者 cgo，与本项目零依赖的原则相冲突。TOML 同时满足两个约束：可读、文件名与格式相符，以及一个我们自己拥有的 parser/writer（`internal/toml`，含测试约 1000 行）。

这些文档按归属和生命周期拆分：

* `mcli.toml` —— 用户的。它必须能挺过一次编辑器会话，所以写入是行级 patch（`toml.SetKeyInSection`），保留注释、顺序和未知键，而不是把结构体重新序列化一遍。
* `state.toml` —— mcli 的。账号（仅身份信息）、实例和 manifest 指针几乎每敲一条命令就会变，所以整份重写。它不含任何机密，因此复制、同步或贴出来求助都是安全的。
* `refresh.toml` / `session.toml` —— 凭据，刻意分成两个文件。Microsoft 的 refresh token 长期有效，值得保护；Minecraft 的 access token 几小时就失效。分开意味着：(a) 值得像密码一样备份的那个文件很少变动，而不是每小时变一次；(b) 删掉 `session.toml` 就能强制换一个新的 Minecraft token，而不碰登录状态；(c) token 泄露的影响面一目了然——只有那个短命文件本身毫无价值。代价：多一个要写文档的文件，以及登录结束时多一次 `SaveTokens` 调用。为了这种按安全边界做的切分，值。
* `last.toml` —— 上一次启动。`mcli launch --last` 不该为了重放参数去解析一份配置，所以那 ~15 个值放进自己那个小文件。

没有迁移代码，也不想要：mcli 在旧形态上没有用户，所以只存在一种形态，旧构建写出的版本会在下一次保存时被直接替换。`state_version`（目前是 1）存在的唯一目的，是让*更新*的 mcli 写出的文件被报告出来，而不是被误读——这是向前保护，不是向后兼容。

代价：五个文件而不是一个，并且 `--config <path>` 只移动配置文件（其余的从它所在目录推导）。两者 README 里都有写。

## 每个账号一个 salt，而不是每台机器一个

加密密钥是 `PBKDF2-SHA256(machine identity, salt)`；salt 就放在它所保护的那个账号旁边。这既让被偷走的文件影响面很小（单单一文件在别处毫无用处），*也*让其余一切保持可迁移：装好的游戏、配置和实例列表在另一台电脑上都能用，只有登录需要重来一次。机器级的 salt 会让整个 state 文件无法迁移，却换不来任何额外好处。

没有遗留数据导入：TOML 之前的 JSON 数据库，以及把 token 内联存储的那个 `state.toml` 版本，都已从代码里消失，覆盖它们的测试也一并删除。

## 实例的游戏目录位于 `versions/<instance>/`

规格里的目录树把实例数据画在 `.minecraft/version/{instance-name}/` 下。保留这个形态、同时保住官方启动器的兼容性，意味着：版本*文件*在 `versions/<version-id>/`（这样官方启动器也能看到它们），每个实例的*游戏数据*在 `versions/<instance>/`，作为 `--gameDir` 传给 JVM。`versions_dir` 可配置（默认 `versions`），README 明确写了这个布局。

代价：如果用户把实例名起得和某个版本 id 完全一样，版本目录和实例目录会重名。这时启动器会通过常规的实例查找给出警告或予以覆盖。

## 接管其他启动器做的安装

一个看不见自己落在哪个 `.minecraft` 上的启动器是不可用的，所以 `instance scan` / `instance import` 接管已有的版本目录，而不是让用户重装 700 MB。两个后果塑造了代码：

* 实例名允许空格和 CJK，因为 `26.3-Fabric 0.20.1` 就是真实 PCL profile 在磁盘上的名字。路径分隔符、Windows 禁用字符和保留设备名仍会被拒绝，结尾的点也一样。
* natives 解包到版本自己的参数所指的位置（`natives/jna`、`natives/lwjgl`、`natives/netty`、`natives/java`），而不只是基础目录。现代 Minecraft 的 libraries（LWJGL、JNA、Netty）在运行时从 classpath 自行解包，但只会解到一个由 JSON 点名的目录——所以启动器必须精确创建这些目录，并把预先解出的文件放到旧式 `java.library.path` 期望的位置。

mcli 仍然不*安装* Fabric、Forge 或任何其他 loader：它运行已经存在的东西。mod 加载不在范围内，这与拒绝启动一个带 mod 的安装不是一回事。

## 镜像失败是降级，不是禁用

每个文件都会先试镜像，这在它正常时是对的，在重定向器卡住时代价高昂：一个 Java 运行时是 ~700 个小文件，每次卡住都要付一次 watchdog 超时。`dl.SourceHealth` 统计每个来源的连续失败次数，达到三次之后就把官方源移到最前面，本次运行余下部分一直如此。镜像仍留在列表里，因为只能访问镜像的网络照样得安装。

代价：一个启发式规则，其阈值没人能证明最优。触发时会在日志里记一次（"… keeps failing; the other sources are tried first"），所以意外的回退是看得见的，而不是悄无声息。

## 多个账号，一个活跃

账号一律以 Minecraft UUID 为键，绝不用名字：同一个玩家可以改名，两个账号也可能在不同区服重名。`mcli account list` 列出所有已存内容，`account use` 切换，`launch --account <name|uuid|label>` 覆盖单次运行，`login --no-switch` 添加账号而不把默认账号从正在用的那个移开。身份信息和凭据在不同的文件里，所以即使 token 文件缺失、或者是在另一台机器上加密的，账号列表也总能渲染出来。

## Java 运行时来自 Mojang 的产品 manifest

`launchermeta.mojang.com/v1/products/java-runtime/<hash>/all.json` 正是官方启动器自己所安装的东西，它由 BMCLAPI 镜像，而且版本 JSON 已经写明它要的 component（`java-runtime-delta` 等）。这严格优于猜 JDK 构建：运行时与 Mojang 随游戏发布的一致。

代价：每个运行时约 430 个小文件（~115 MiB），而不是一个压缩包。下载器的 worker pool 处理得过来，而且整批只确认一次。Adoptium 的 tarball 保留为兜底，用于 Mojang 不发布的平台（例如 Linux arm64）。

## 优先用本地 Java，绝不静默安装

匹配规则是「主版本精确，否则选最小的更新版本」。没有明确的 Y/N（或 `--yes`），什么都不会下载。`java_path` 可以固定一个运行时，但仍会对照版本的要求做检查，所以一个固定值不会悄悄用太旧的 JVM 启动游戏。

## 每次下载都是一个有序的候选列表

BMCLAPI v2 端点会把部分请求重定向到常常很慢的 `*.sakura.sld.tw` 节点；官方 Mojang 主机在某些网络里被限速、在另一些网络里很快。这里不做全局的「镜像开/关」开关，而是每个文件自带候选列表（镜像在前、官方在后），下载器逐个走一遍，并重试整个列表。卡住的传输在 20 秒后中止，换下一个候选。

代价：运气不好时单个文件可能被尝试好几次，表现为日志里的 `WARN` 行。这是刻意的：对一个正在退化的镜像保持沉默会更糟。

## Assets：索引走镜像，object 按 hash 取

BMCLAPI 不提供 `/assets/indexes/<id>.json`（404），但镜像了 `piston-meta` 的路径，所以索引通过 `/v1/packages/<sha1>/<id>.json` 抓取，object 通过 `/assets/<first2>/<hash>` 抓取。object 是内容寻址的，这让「是否已存在」的检测很便宜，也让共享缓存在实例之间、启动器之间都是安全的。

## 用 AES-GCM + 机器派生密钥，而不是操作系统钥匙串

规格要求 token 用由本机设备信息派生的密钥加密，且不依赖系统钥匙串。这里用 PBKDF2-SHA256（20 万次迭代）作用于 `machine-id` + 产品 UUID + hostname + 用户名，并以随机字节作 salt 存在 `refresh.toml` 里。后果 README 里有写，出错时也会显示：复制到另一台机器的 `refresh.toml` 无法解密，而只偷走这个文件本身也拿不到 refresh token。

## i18n 用点分 id 作键，英文是唯一事实来源

翻译以键（`dl.sha_mismatch`）为准，绝不以英文原文为键，所以措辞可以自由改动。有两个测试守住这份约定：仓库里任何地方用到的键都必须存在（`TestSourceKeysExist`），两份目录必须拥有完全相同的键和完全相同的 printf 动词（`TestCatalogsInSync`、`TestFormatVerbsMatch`）。语言判定顺序：`--lang` → `mcli.toml:language` → `MCLI_LANG` → `LC_ALL`/`LC_MESSAGES`/`LANG` → 英文。

## 只有参数，没有交互式向导

每个动作都能用一条命令加若干参数表达，这正是这个工具可以被 cron 或 CI runner 脚本化的原因。唯一的交互元素是大下载前的 Y/N 确认，而 `--yes` 会关掉它们。launch 上的 `--dry-run` 打印出确切的 java 命令行，而不碰任何游戏文件。

## 日志：结果走 stdout，诊断走 stderr

结果（表格、路径、状态）走 stdout，这样可以接管道；分级诊断走 stderr 和每日日志文件。游戏自身的输出写到 `logs/game-<instance>-<timestamp>.log`，绝不与启动器的输出交织在一起。
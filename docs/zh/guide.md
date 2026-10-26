# 指南

## 安装

构建单个二进制（Go 1.26 或更新版本，仅使用标准库）：

```sh
git clone https://github.com/Kailoinf/MCL-CLI
cd MCL-CLI
go build -o mcli .
```

把它放到 `PATH` 上的某个位置。没有运行时依赖：不捆绑 Java，不需要 Python，也不需要 node。如果某个版本需要的 Java 运行时缺失，mcli 会自行安装。

## 首次运行

```sh
mcli login                       # opens the Microsoft page in a browser
mcli create vanilla --version latest
mcli launch vanilla
```

`launch` 会安装该版本需要的东西，然后启动游戏。游戏的输出写入 `<mcli home>/logs/game-<timestamp>.log`；`mcli doctor` 会打印最新的那一份。

## 文件都在哪里

```
~/.mcl-cli/     mcli.toml (config) · state.toml (identity, no secrets)
                refresh.toml (long-lived tokens) · session.toml (short-lived)
                last.toml (previous launch) · jdk/ · cache/ · logs/
~/.minecraft/   libraries/ · assets/ · versions/<version>/ · versions/<instance>/
```

`MCLI_HOME` 和 `MCLI_MINECRAFT` 可以覆盖这两个目录，靠它们才能为测试保留第二套互相隔离的配置。`mcli config path` 会打印这两个目录。

## 账号

可以同时登录多个账号。`mcli account list` 列出它们，`use` 切换默认账号，`launch --account <name>` 在单次运行中覆盖默认账号，`--no-switch` 能在不改变默认账号的前提下添加一个账号。凭据用从本机派生出的密钥加密，因此这两个 token 文件无法在别处解密 —— 而 `state.toml` 始终保持可移植。

## 实例

实例是一个具名目录，拥有自己的存档、选项和设置；体积大的共享部分（libraries、assets、版本文件）只在 `~/.minecraft` 中保存一份。`mcli instance set` 修改内存、Java 和启动参数；`mcli instance copy` 克隆一个实例；`mcli instance scan` 列出磁盘上的每一个版本目录，包括其他启动器创建的目录，`mcli instance import` 则原地接管其中一个 —— 文件和存档都留在原处。

## 服务器

```sh
mcli server create survival --version latest --port 25566 --xmx 4G
mcli server start survival              # attached: the console is yours, Ctrl+C saves and stops
mcli server start survival --detach     # background, pid recorded
mcli server attach survival             # follow output and type commands; Ctrl+C only leaves
mcli server cmd survival say hello      # one command, no session
mcli server stop survival               # SIGINT, so the world is saved
```

服务器位于 `<minecraft_dir>/servers/<name>`。`eula.txt` 和 `server.properties` 只创建一次，之后绝不覆盖；Minecraft EULA 的 URL 会被打印出来，必须接受（`--eula` 跳过的是询问，不是这条提示）。

## Fabric

```sh
mcli fabric list latest                                      # 查看某个游戏版本可用的加载器
mcli create modded --version latest --loader fabric          # 最新稳定加载器
mcli create pinned --version latest --loader fabric --loader-version 0.16.9
mcli instance set modded --loader vanilla                     # 也能切回原版
```

模组加载器是**版本上的一个选项**，不是单独一条命令：`create` 与 `instance set` 上的
`--loader vanilla|fabric`，需要固定某个构建时再加 `--loader-version`。取值超出这个选择
范围会直接报错并列出可选值，不会悄悄退回默认。Fabric 发布的 profile 会写入 mcli 自己的
`versions/` 目录，因此之后离线也能用，并且可以像其他任何版本一样启动、修复和诊断。

## 模组与资源

mcli 用 Modrinth 作为来源 —— 它不需要 API key，接口也小到可以如实包一层。

```sh
mcli search sodium --version 1.21.4 --loader fabric    # 一次查询、一页结果
mcli search faithful --type resourcepack --version 1.21.4
mcli add sodium --instance modded                      # 下载匹配的那个文件
mcli add faithful --instance modded --dry-run          # 先看会放到哪
mcli mods modded                                       # 看看已经装了什么
```

* 搜索故意做薄：一次查询、一页结果（翻页用 `--limit`、`--offset`），只显示要装的
  slug、类型、下载量、作者和标题；没有需要长期维护的浏览界面。
* `add` 会挑出与实例的游戏版本和加载器匹配的最新版本，按 API 给的 SHA-1 下载它的
  主文件，再按项目类型放进正确目录：`mods/`、`resourcepacks/`、`shaderpacks/`、
  `datapacks/`。
* 模组需要加载器。往原版实例里装模组会被拒绝，并给出解决办法（`--loader fabric`），
  而不是把 Fabric 的 jar 丢进一个根本加载不了它的游戏；你确定要这么做时可以用
  `--loader` 覆盖。
* 必需的前置依赖会被打印出来，但不会自动安装：启动器悄悄多拉三个项目，只会
  留下一堆没人能复核的结果。

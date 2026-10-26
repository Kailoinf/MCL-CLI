# mcli —— 命令行 Minecraft 启动器

配合微软正版账号使用的 Minecraft: Java 版命令行启动器，单个 Go 二进制、零第三方依赖。

## 安装

```sh
git clone https://github.com/Kailoinf/MCL-CLI
cd MCL-CLI && go build -o mcli .
```

## 上手

```sh
mcli login                            # 打开浏览器登录微软账号
mcli create vanilla --version latest  # 用当前正式版建一个实例
mcli launch vanilla                   # 缺什么装什么，然后启动
mcli doctor --all                     # 体检：有没有缺失或损坏
```

## 服务器

```sh
mcli server create survival --version latest --port 25566 --xmx 4G
mcli server start survival          # 前台：控制台就是你的，Ctrl+C 会保存并停止
mcli server start survival --detach # 后台：记录 pid
mcli server attach survival         # 接上后台服务器的控制台；Ctrl+C 只是离开
mcli server stop survival           # 发 SIGINT，存档会保存
```

EULA 不会替你同意：先打印 `https://aka.ms/MinecraftEULA`，再要求按 Y 确认。

## Fabric

```sh
mcli fabric list latest                                      # 查看某个游戏版本可用的加载器
mcli create modded --version latest --loader fabric          # 安装最新稳定加载器
mcli instance set modded --loader vanilla                     # 也能切回原版
```

## 模组与资源

```sh
mcli search sodium --version 1.21.4 --loader fabric   # 在 Modrinth 上搜索
mcli add sodium --instance modded                    # 装进实例的 mods/
mcli mods modded                                     # 看看已经装了什么
```

## 继续读

[指南](/zh/guide) · [命令参考](/zh/commands) · [架构](/zh/architecture) ·
[设计决策](/zh/design-decisions) · [实时状态](/zh/status)。
英文页面对应 [Guide](/guide)、[Commands](/commands)、[Architecture](/architecture)、
[Design decisions](/design-decisions)、[Status](/status)；仓库里还有
`README.zh-CN.md` 速览和 `skill/SKILL.md` 开发手册。

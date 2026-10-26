# mcli - Minecraft Launcher CLI 需求文档
> 语言：Go
> 参考项目：
> - [https://github.com/MrShieh-X/console-minecraft-launcher](https://github.com/MrShieh-X/console-minecraft-launcher)
> - [https://github.com/Meloong-Git/PCL](https://github.com/Meloong-Git/PCL)
> - [https://github.com/PCL-Community/PCL-CE](https://github.com/PCL-Community/PCL-CE)
> 说明：版本解析、Java匹配、token处理等不清楚的逻辑，参考上面项目

## 概述
纯命令行MC Java版原版启动器，无GUI，仅支持微软正版账号，暂不支持Mod加载器。

### 核心约束
1. 仅微软正版登录，无离线模式
2. 默认使用 BMCLAPI 镜像，回退官方源
3. `.minecraft` 路径与官方启动器保持一致
4. libraries / assets / 版本清单全局共享缓存，实例数据隔离
5. 纯 flag 驱动，无交互式向导
6. 终端简洁输出，完整日志写入文件

## 目录结构（与其他启动器一致）
```
.minecraft
├─ libraries/         # 全局共享库
├─ assets/            # 全局共享资源
└─ version/
   └─ {instance-name}/   # 实例独立目录：saves、options、resourcepacks等


.mcl-cli/
└─ mcli.db         #配置，包括.minecraft的位置、各项配置
```


## 账号凭证
AES对称加密存储token，密钥由本机设备信息派生，不依赖系统密钥链，只在本机有效。

## 下载&版本
- 支持 release / snapshot / alpha / beta
- 版本清单带TTL，过期静默自动刷新；`mcli refresh` 强制刷新
- SHA1校验失败自动重试，重试耗尽报错退出
- 任何大文件（游戏、Java）下载前必须Y/N确认

## Java运行时
根据版本json自动匹配所需Java，匹配逻辑参考PCL等项目；优先本机Java，找不到经用户确认后从BMCLAPI下载配套Java，不内嵌Java。存储到.mcl-cli/jdk下

## 实例
多实例隔离，每个实例独立存档与设置，资源文件全局复用。

## JVM内存
自动根据系统可用内存推荐Xmx/Xms，支持flag手动覆盖。

## 子命令
```

mcli login
mcli logout
mcli list
mcli create <name>
mcli delete <name>
mcli launch <name> [flags]
mcli refresh
mcli config

```
支持 `--last` 复用上次启动参数。，如：`mcli launch --last`

## 配置 mcli.db/config表
- download_retry_times
- http_timeout
- manifest_auto_refresh
- manifest_ttl_hours
- bmclapi_endpoint
- log_level_file
- log_level_terminal

## 本期不实现
- Fabric / Forge / Quilt
- Mod管理、资源包安装

## 不实现
- 离线账号
- GUI

# For AGENT
善写文档，擅更新AGENTS.md
常commit，常push。
请自行创建远程仓库Kailoinf/MCL-CLI
检查.gitignore，不push敏感文件
先PULL参考项目，先读参考源码。
删除参考项目的UI部分，避免上下文超标
擅长使用子AGENT
为项目编写详细文档